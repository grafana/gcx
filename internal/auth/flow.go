// Package auth implements the browser-based OAuth PKCE authentication flow for gcx.
// This file is based heavily on assistant-cli-internal/internal/tunnel/auth/flow.go.
package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/deeplink"
)

//go:embed templates/*.html
var templateFS embed.FS

const maxResponseBytes = 10 << 20 // 10 MB

// Result contains the result of a successful authentication flow.
type Result struct {
	// Token is the gat_ access token for API authentication.
	Token string

	// Email is the user's email address.
	Email string

	// DeviceName is the device name (if provided).
	DeviceName string

	// APIEndpoint is the proxy base URL for forwarding requests.
	APIEndpoint string

	// ExpiresAt is the token expiration time in RFC3339 format.
	ExpiresAt string

	// RefreshToken is the gar_ refresh token for obtaining new access tokens.
	RefreshToken string

	// RefreshExpiresAt is the refresh token expiration time in RFC3339 format.
	RefreshExpiresAt string

	// InstanceEndpoint is the endpoint returned by the grafana instance itself
	// Only used if the endpoint isn't available during auth (e.g. signing in through grafana.com)
	InstanceEndpoint string
}

// defaultScopes are the scopes requested by gcx.
var defaultScopes = []string{"grafana-api:read", "grafana-api:write", "grafana-api:delete", "assistant:a2a", "assistant:chat"} //nolint:gochecknoglobals

// Options configures the authentication flow.
type Options struct {
	// Port specifies a fixed port for the callback server.
	// If 0, an available port will be found automatically.
	Port int

	// BindAddress specifies the address to bind the callback server to.
	// Defaults to "127.0.0.1".
	BindAddress string

	// Scopes specifies the token scopes to request.
	// If empty, DefaultScopes are used.
	Scopes []string

	// Writer is the output writer for user-facing messages.
	// Defaults to os.Stderr.
	Writer io.Writer

	// Manual completes the flow without a callback server. gcx prints the
	// login URL and reads the redirect URL that the user copies from the
	// browser address bar. Use it when the browser runs on another computer,
	// for example when gcx runs over SSH.
	Manual bool

	// Reader supplies the pasted redirect URL in manual mode.
	// Defaults to os.Stdin.
	Reader io.Reader

	// LaunchOrigin is the Grafana Cloud portal origin (scheme and host) that
	// serves the stack launcher when no stack endpoint is known yet. Empty
	// means https://grafana.com. It must be a trusted Grafana Cloud origin.
	LaunchOrigin string

	// Signup opens the Grafana Cloud account creation page first, for a person
	// who has no account yet. That page returns to the stack launcher once the
	// account and its first stack exist. It applies only when no stack
	// endpoint is known.
	Signup bool

	// ManualCommand, when set, is the exact command that the remote session
	// hint tells the user to run for the manual flow. It applies only when no
	// stack endpoint is known. Empty selects gcx login --cloud --oauth-manual.
	ManualCommand string

	// ReopenOnEnter lets the user press Enter in the terminal to open the
	// login page again while gcx waits, for example after the browser lost it
	// during signup. It applies only when no stack endpoint is known, and only
	// in a local terminal session: over SSH the paste watcher owns the
	// terminal.
	ReopenOnEnter bool
}

// openBrowser opens a URL in the user's browser. It is a variable so tests can
// count the opens without starting a real browser.
var openBrowser = deeplink.OpenWithStatus //nolint:gochecknoglobals // test seam

// Flow manages the browser-based authentication process.
type Flow struct {
	endpoint string
	opts     Options
	writer   io.Writer
	reader   io.Reader
}

// NewFlow creates a new authentication flow for the given Grafana endpoint.
func NewFlow(endpoint string, opts Options) *Flow {
	if opts.BindAddress == "" {
		opts.BindAddress = "127.0.0.1"
	}
	if len(opts.Scopes) == 0 {
		opts.Scopes = defaultScopes
	}
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	r := opts.Reader
	if r == nil {
		r = os.Stdin
	}
	return &Flow{endpoint: endpoint, opts: opts, writer: w, reader: r}
}

// Run executes the authentication flow.
func (f *Flow) Run(ctx context.Context) (*Result, error) {
	if f.opts.LaunchOrigin != "" && f.endpoint == "" {
		if err := ValidateLaunchOrigin(f.opts.LaunchOrigin); err != nil {
			return nil, err
		}
	}
	if f.opts.Manual {
		if f.opts.Port != 0 {
			return nil, errors.New("manual OAuth does not use a callback port")
		}
		return f.runManual(ctx)
	}
	return f.runWithCallbackServer(ctx)
}

// runManual completes the flow without a callback server. The browser cannot
// reach the callback address, so the user copies the redirect URL out of the
// address bar and pastes it here.
func (f *Flow) runManual(ctx context.Context) (*Result, error) {
	state, codeVerifier, codeChallenge, err := newFlowSecrets()
	if err != nil {
		return nil, err
	}

	authURL := f.buildEntryURL(f.buildAuthURL(manualCallbackPort, state, codeChallenge))
	// No callback server runs here, so no route can race the paste. A nil guard
	// always grants the claim.
	return runManualPaste(ctx, f.writer, f.reader, authURL, f.launcherBrowserStep(), verificationCode(codeChallenge),
		func(q url.Values) (*Result, *callbackError) {
			return handleCallbackParams(ctx, q, state, codeVerifier, nil)
		})
}

func (f *Flow) runWithCallbackServer(ctx context.Context) (*Result, error) {
	listener, port, err := listenOnCallbackPort(ctx, f.opts.BindAddress, f.opts.Port)
	if err != nil {
		if f.opts.Port == 0 {
			return nil, fmt.Errorf("no available port: %w", err)
		}
		return nil, err
	}

	state, codeVerifier, codeChallenge, err := newFlowSecrets()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	resultCh := make(chan *Result, 1)
	errCh := make(chan error, 1)
	// The callback server and the paste reader accept the same single-use code,
	// so one guard decides which route exchanges it.
	guard := &exchangeGuard{}
	server := f.startCallbackServer(ctx, listener, state, codeVerifier, guard, resultCh, errCh)

	defer func() { //nolint:contextcheck // intentionally use Background for graceful shutdown after ctx cancellation
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	authURL := f.buildAuthURL(port, state, codeChallenge)
	entryURL := f.buildEntryURL(authURL)
	code := verificationCode(codeChallenge)

	fmt.Fprintln(f.writer, "Opening browser to authenticate...")
	fmt.Fprintf(f.writer, "If browser doesn't open, visit:\n  %s\n\n", entryURL)

	fmt.Fprintf(f.writer, "Verification code: %s\n", code)
	fmt.Fprintln(f.writer, "Check that this code matches what is shown in the browser before approving.")
	fmt.Fprintln(f.writer)

	if opened, err := openBrowser(entryURL); err != nil {
		fmt.Fprintln(f.writer, "(Could not open browser automatically)")
	} else if !opened {
		fmt.Fprintln(f.writer, "(Browser launch skipped in agent mode — open the URL above manually)")
	}

	f.printLauncherSteps()

	// Over SSH the browser cannot reach the callback address. Accept a pasted
	// redirect URL alongside the callback so the user never has to restart.
	paste := startPasteWatcher(f.writer, port)
	if paste == nil && f.opts.ReopenOnEnter && f.endpoint == "" {
		paste = startReopenWatcher(f.writer)
	}
	defer paste.Close()
	switch {
	case paste == nil:
		manualCommand := "gcx login --oauth-manual"
		switch {
		case f.endpoint == "" && f.opts.ManualCommand != "":
			manualCommand = f.opts.ManualCommand
		case f.endpoint == "":
			// Without --cloud a rerun with no server has no launcher to go to.
			manualCommand = "gcx login --cloud --oauth-manual"
		}
		printRemoteSessionHint(f.writer, port, manualCommand)
		fmt.Fprintln(f.writer, "Waiting for authentication...")
		if f.endpoint == "" {
			// No Enter shortcut here (agent mode, a non-interactive run, or
			// no terminal gcx can watch, as on Windows), but the printed URL
			// recovers a lost page just as well.
			fmt.Fprintln(f.writer, "If you lose the page, open the URL above again. Press Ctrl-C to cancel.")
		}
	case paste.reopen:
		fmt.Fprintln(f.writer, "gcx is waiting. If you lose the page, press Enter to open it again. Press Ctrl-C to cancel.")
	}

	// Reopening goes to the launcher, never to the signup page: by the time a
	// page is lost the account usually exists, and the launcher takes a
	// signed-in user straight to their stack. The URL carries the same state,
	// challenge and port, so every open tab stays valid.
	var lastReopen time.Time
	reopen := func() {
		// A claimed guard means the callback arrived and its token exchange
		// is running, so a new consent tab could only end on 410 Gone.
		if guard.taken.Load() || ctx.Err() != nil {
			return
		}
		// Repeated Enter presses open one tab, not one per press.
		if time.Since(lastReopen) < reopenDebounce {
			return
		}
		lastReopen = time.Now()
		fmt.Fprintf(f.writer, "\nOpening the login page again:\n  %s\nVerification code: %s\n", authURL, code)
		if _, err := openBrowser(authURL); err != nil {
			fmt.Fprintln(f.writer, "(Could not open browser automatically)")
		}
	}

	return awaitCallbackOrPaste(ctx, f.writer, paste, reopen, resultCh, errCh,
		func(q url.Values) (*Result, *callbackError) {
			return handleCallbackParams(ctx, q, state, codeVerifier, guard)
		})
}

// reopenDebounce is the shortest interval between two reopened login pages.
const reopenDebounce = 2 * time.Second

// printLauncherSteps says what the browser asks for when no stack endpoint is
// known, because the signup detour can take several minutes and several pages.
func (f *Flow) printLauncherSteps() {
	if f.endpoint != "" {
		return
	}
	if f.opts.Signup {
		fmt.Fprintln(f.writer, "In the browser:")
		fmt.Fprintln(f.writer, "  1. Create your Grafana Cloud account and verify your email.")
		fmt.Fprintln(f.writer, "  2. Create your first stack. It can take a few minutes to start.")
		fmt.Fprintln(f.writer, "  3. Approve \"Connect gcx\" after checking the verification code.")
	} else {
		fmt.Fprintln(f.writer, "In the browser, sign in to Grafana Cloud, choose a stack, and approve \"Connect gcx\".")
	}
	fmt.Fprintln(f.writer)
}

// launcherBrowserStep is the manual route's version of printLauncherSteps: one
// numbered step, placed between opening the URL and approving, so the manual
// instructions read in the order the user acts. It is empty when a stack
// endpoint is known.
func (f *Flow) launcherBrowserStep() string {
	switch {
	case f.endpoint != "":
		return ""
	case f.opts.Signup:
		return "Create your Grafana Cloud account, verify your email, and create your first stack.\n" +
			"   The stack can take a few minutes to start."
	default:
		return "Sign in to Grafana Cloud and choose a stack."
	}
}

// defaultLaunchOrigin serves the stack launcher when no LaunchOrigin is set.
const defaultLaunchOrigin = "https://grafana.com"

// launchOrigin returns the Grafana Cloud portal origin for the no-stack path.
func (f *Flow) launchOrigin() string {
	if f.opts.LaunchOrigin == "" {
		return defaultLaunchOrigin
	}
	return strings.TrimSuffix(f.opts.LaunchOrigin, "/")
}

// ValidateLaunchOrigin accepts a trusted Grafana Cloud portal origin that
// consists of a scheme and host only. Flow.Run checks LaunchOrigin with it
// before the flow prints or binds anything.
func ValidateLaunchOrigin(origin string) error {
	if err := validateGCOMURL(origin); err != nil {
		return fmt.Errorf("invalid Grafana Cloud URL for the browser login: %w", err)
	}
	u, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid Grafana Cloud URL for the browser login: %w", err)
	}
	if u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("invalid Grafana Cloud URL for the browser login: %q must contain only a scheme and host", origin)
	}
	return nil
}

// buildAuthURL renders the plugin consent URL for the given callback port.
// Without a stack endpoint it renders the Grafana Cloud stack launcher, which
// forwards the same path and query to the stack the user picks.
func (f *Flow) buildAuthURL(port int, state, codeChallenge string) string {
	authEndpoint := strings.TrimSuffix(f.endpoint, "/")
	if authEndpoint == "" {
		authEndpoint = f.launchOrigin() + "/launch"
	}

	authURL := fmt.Sprintf("%s/a/grafana-assistant-app/cli/auth?callback_port=%d&state=%s&code_challenge=%s&code_challenge_method=S256",
		authEndpoint, port, url.QueryEscape(state), url.QueryEscape(codeChallenge))

	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		authURL += "&device_name=" + url.QueryEscape(hostname)
	}

	if len(f.opts.Scopes) > 0 {
		authURL += "&scopes=" + url.QueryEscape(strings.Join(f.opts.Scopes, ","))
	}

	return authURL
}

// buildEntryURL returns the URL that the browser opens first. It is authURL
// itself, except on the no-stack signup path. There it is the Grafana Cloud
// account creation page, carrying the launcher path and query as its
// grafana.com-relative return target: the signup pages keep that target
// through email verification and first-stack creation, then send the browser
// to the launcher, which forwards to the new stack's consent page.
func (f *Flow) buildEntryURL(authURL string) string {
	if !f.opts.Signup || f.endpoint != "" {
		return authURL
	}
	u, err := url.Parse(authURL)
	if err != nil {
		return authURL
	}
	returnTo := u.EscapedPath() + "?" + u.RawQuery
	return f.launchOrigin() + "/auth/sign-up/create-user?to=" + url.QueryEscape(returnTo)
}

// newFlowSecrets generates the CSRF state and the PKCE verifier and challenge,
// in that order.
func newFlowSecrets() (string, string, string, error) {
	state, err := generateState()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate state: %w", err)
	}

	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate PKCE code verifier: %w", err)
	}

	return state, codeVerifier, generateCodeChallenge(codeVerifier), nil
}

func (f *Flow) startCallbackServer(ctx context.Context, listener net.Listener, expectedState, codeVerifier string, guard *exchangeGuard, resultCh chan<- *Result, errCh chan<- error) *http.Server {
	return newCallbackServer(listener, expectedState, errCh, func(w http.ResponseWriter, r *http.Request) bool {
		result, cerr := handleCallbackParams(ctx, r.URL.Query(), expectedState, codeVerifier, guard)
		if cerr != nil {
			return answerCallbackError(w, f.writer, cerr, errCh)
		}

		resultCh <- result
		renderSuccessPage(w)
		return true
	})
}

// foreignCallbackMessage is shown in the browser for a callback that does not
// belong to the login waiting in the terminal. It names the likely cause
// instead of an attack, and says the waiting login is unaffected, because it is.
const foreignCallbackMessage = "This page does not belong to the login waiting in your terminal, so gcx ignored it. " +
	"It is most likely from an earlier login attempt. " +
	"The waiting login is still running: finish it from the page it opened, " +
	"or press Ctrl-C in the terminal and run the command again."

// incompleteCallbackMessage is shown in the browser for a callback of the
// waiting login that lacks what the token exchange needs. The %s names what is
// wrong. The login keeps waiting, so the page says so instead of reporting a
// failure.
const incompleteCallbackMessage = "This page is missing part of the login (%s), so gcx ignored it. " +
	"The login waiting in your terminal is still running: finish it from the page it opened, " +
	"or press Ctrl-C in the terminal and run the command again."

// answerCallbackError answers a callback that handle rejected and reports
// whether the rejection ends the login. A retryable rejection spent nothing, so
// the login keeps waiting for the real callback. It still says so in the
// terminal on out: when the consent page itself sends the incomplete callback,
// every attempt fails the same way, and a silent wait would hide why.
func answerCallbackError(w http.ResponseWriter, out io.Writer, cerr *callbackError, errCh chan<- error) bool {
	switch {
	case errors.Is(cerr.err, errExchangeClaimed):
		// The paste route won the race, and the login is complete. Do not send
		// to errCh: that would end a flow that succeeded.
		renderSuccessPage(w)
		return true
	case cerr.retryable:
		fmt.Fprintf(out, "\nThe browser sent this login's callback without what gcx needs (%s), so gcx ignored it and is still waiting. "+
			"If this happens again, press Ctrl-C and report it.\n", cerr.page)
		renderNoticePage(w, http.StatusBadRequest, "Nothing to do here", fmt.Sprintf(incompleteCallbackMessage, cerr.page))
		return false
	default:
		errCh <- cerr.err
		renderCallbackFailure(w, cerr)
		return true
	}
}

// newCallbackServer binds a single-use /callback handler to listener and starts
// serving in a goroutine. Serve errors are reported on errCh.
//
// Only a GET that carries expectedState may run handle. handle reports whether
// the request ended the login; once one has, a replayed callback gets 410 Gone.
// Anything else is answered and ignored, so it cannot end the login: every
// local process can reach the loopback listener, and a tab from an earlier
// attempt lands on the same port when the new attempt picks it again.
//
// Requests run handle one at a time, so a second callback waits for the token
// exchange of the first and then sees whether it ended the login.
func newCallbackServer(listener net.Listener, expectedState string, errCh chan<- error, handle func(http.ResponseWriter, *http.Request) bool) *http.Server {
	var mu sync.Mutex
	consumed := false

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// An explicit check, not a "GET /callback" ServeMux pattern: that
		// pattern also matches HEAD, which would let a HEAD request run the
		// token exchange as a side effect.
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !callbackBelongsToFlow(r.URL.Query(), expectedState) {
			renderNoticePage(w, http.StatusBadRequest, "Nothing to do here", foreignCallbackMessage)
			return
		}

		mu.Lock()
		defer mu.Unlock()
		if consumed {
			http.Error(w, "Authentication already processed", http.StatusGone)
			return
		}
		consumed = handle(w, r)
	})

	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	return server
}

var allowedDomainSuffixes = []string{ //nolint:gochecknoglobals
	".grafana.net",
	".grafana-dev.net",
	".grafana-ops.net",
}

// ValidateEndpointURL checks that the given endpoint URL is a trusted Grafana domain
// or a local address. Returns an error if the URL is untrusted.
func ValidateEndpointURL(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Host == "" {
		return errors.New("endpoint has no host")
	}

	hostname := u.Hostname()

	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return nil
	}

	if u.Scheme != "https" {
		return fmt.Errorf("endpoint must use HTTPS, got %q", u.Scheme)
	}

	for _, suffix := range allowedDomainSuffixes {
		if strings.HasSuffix(hostname, suffix) {
			return nil
		}
	}

	return fmt.Errorf("endpoint host %q is not a trusted Grafana domain", hostname)
}

var allowedGCOMHosts = []string{ //nolint:gochecknoglobals
	"grafana.com",
	"grafana-dev.com",
	"grafana-ops.com",
}

// validateGCOMURL checks that the given URL points at a trusted Grafana Cloud
// platform (GCOM) domain or a local address. Unlike ValidateEndpointURL, which
// guards per-stack *.grafana.net endpoints, this validates the grafana.com
// family used by the cloud login flow. Returns an error if the URL is untrusted.
func validateGCOMURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Host == "" {
		return errors.New("URL has no host")
	}

	hostname := u.Hostname()

	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return nil
	}

	if u.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS, got %q", u.Scheme)
	}

	if slices.Contains(allowedGCOMHosts, hostname) {
		return nil
	}

	return fmt.Errorf("URL host %q is not a trusted Grafana Cloud domain", hostname)
}

// ExchangeStatusError reports a token exchange that the Grafana Cloud backend
// answered with a status other than 200. The response body is never included:
// it may echo request data. The authorization code is spent either way, so
// recovery always means a new login.
type ExchangeStatusError struct {
	StatusCode int
	Path       string
}

func (e *ExchangeStatusError) Error() string {
	return fmt.Sprintf("oauth token exchange failed: status %d from %s", e.StatusCode, e.Path)
}

type exchangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		Token            string `json:"token"`
		Tenant           string `json:"tenant"`
		Email            string `json:"email"`
		ExpiresAt        string `json:"expires_at"`
		APIEndpoint      string `json:"api_endpoint"`
		RefreshToken     string `json:"refresh_token"`
		RefreshExpiresAt string `json:"refresh_expires_at"`
	} `json:"data"`
}

func exchangeCodeForToken(ctx context.Context, endpoint, code, codeVerifier string) (*exchangeResponse, error) {
	body, err := json.Marshal(map[string]string{
		"code":          code,
		"code_verifier": codeVerifier,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal exchange request: %w", err)
	}

	exchangeURL := strings.TrimSuffix(endpoint, "/") + "/api/cli/v1/auth/exchange"

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectEndpoint := req.URL.Scheme + "://" + req.URL.Host
			if err := ValidateEndpointURL(redirectEndpoint); err != nil {
				return fmt.Errorf("redirect to untrusted URL blocked: %w", err)
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchange request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read exchange response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &ExchangeStatusError{StatusCode: resp.StatusCode, Path: req.URL.Path}
	}

	var result exchangeResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse exchange response: %w", err)
	}

	if result.Data.Token == "" {
		return nil, errors.New("exchange response missing token")
	}
	if result.Data.APIEndpoint == "" {
		return nil, errors.New("exchange response missing api_endpoint")
	}
	if err := ValidateEndpointURL(result.Data.APIEndpoint); err != nil {
		return nil, fmt.Errorf("exchange response contains untrusted api_endpoint: %w", err)
	}

	return &result, nil
}

func listenOnCallbackPort(ctx context.Context, bindAddress string, fixedPort int) (net.Listener, int, error) {
	var lc net.ListenConfig
	if fixedPort != 0 {
		listener, err := lc.Listen(ctx, "tcp", fmt.Sprintf("%s:%d", bindAddress, fixedPort))
		if err != nil {
			return nil, 0, fmt.Errorf("callback port %d unavailable: %w", fixedPort, err)
		}
		return listener, fixedPort, nil
	}

	for port := 54321; port < 54400; port++ {
		listener, err := lc.Listen(ctx, "tcp", fmt.Sprintf("%s:%d", bindAddress, port))
		if err == nil {
			return listener, port, nil
		}
	}
	return nil, 0, errors.New("no available port in range 54321-54399")
}

func generateState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateCodeChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func verificationCode(codeChallenge string) string {
	raw, err := base64.RawURLEncoding.DecodeString(codeChallenge)
	if err != nil || len(raw) < 4 {
		return codeChallenge[:8]
	}
	h := hex.EncodeToString(raw[:4])
	return h[:4] + "-" + h[4:]
}

// StripControlChars sanitises errors to stop potentially malicious errors from
// being interpolated.
func StripControlChars(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// renderSuccessPage answers the callback that delivered the credential. It
// claims only the browser step: gcx still validates the connection and saves
// it afterwards, and either can fail, so the terminal reports the result.
func renderSuccessPage(w http.ResponseWriter) {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/success.html"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

// renderCallbackFailure answers a callback that ended the login without a
// token. A cancellation is the user's own choice, so it gets a neutral page
// rather than the failure page.
func renderCallbackFailure(w http.ResponseWriter, cerr *callbackError) {
	if errors.Is(cerr.err, ErrBrowserCancelled) {
		renderNoticePage(w, http.StatusOK, "Login cancelled", "gcx stopped waiting for this login. The terminal says what to do next.")
		return
	}
	renderErrorPage(w, cerr.page)
}

func renderNoticePage(w http.ResponseWriter, status int, title, message string) {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/notice.html"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct{ Title, Message string }{Title: title, Message: message}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func renderErrorPage(w http.ResponseWriter, errMsg string) {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/error.html"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	data := struct{ Error string }{Error: errMsg}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}
