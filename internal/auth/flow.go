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
}

// Flow manages the browser-based authentication process.
type Flow struct {
	endpoint string
	opts     Options
	writer   io.Writer
	reader   io.Reader
	// acquireTimeout bounds the wait for a callback that belongs to this flow.
	// Set from defaultCallbackAcquireTimeout; tests shorten it per instance.
	acquireTimeout time.Duration
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
	return &Flow{endpoint: endpoint, opts: opts, writer: w, reader: r, acquireTimeout: defaultCallbackAcquireTimeout}
}

// Run executes the authentication flow.
func (f *Flow) Run(ctx context.Context) (*Result, error) {
	// Checked up front, not left to the bind, so an unusable port fails before
	// any browser or network side effect on either flow.
	if err := ValidateCallbackPort(f.opts.Port); err != nil {
		return nil, err
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

	authURL := f.buildAuthURL(manualCallbackPort, state, codeChallenge)
	printManualInstructions(f.writer, authURL, verificationCode(codeChallenge))

	// Deliberately unbounded apart from ctx. readLineContext cannot cancel the
	// read it wraps — a blocking read on the process's own terminal is not
	// interruptible in Go — so a deadline here would return while an abandoned
	// goroutine still owned the terminal. The URL the user was midway through
	// pasting would then land in the shell, writing a live authorization code
	// into shell history. Ctrl-C is the bound for manual mode.
	line, err := readLineContext(ctx, f.reader)
	if err != nil {
		return nil, err
	}

	values, err := ParseCallbackInput(line)
	if err != nil {
		return nil, fmt.Errorf("cannot read the redirect URL: %w", err)
	}

	result, cerr := handleCallbackParams(ctx, values, state, codeVerifier)
	if cerr != nil {
		return nil, pasteRejection(cerr.err)
	}

	fmt.Fprintln(f.writer, manualCallbackHygieneNotice)
	return result, nil
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
	ignored := make(chan struct{}, 1)
	// One arbiter for every path that can complete this flow: the HTTP
	// handler below, the paste watcher further down, and the deadline.
	arb := newCallbackArbiter(f.acquireTimeout)
	defer arb.stop()
	server := f.startCallbackServer(ctx, listener, state, codeVerifier, arb, resultCh, errCh, ignored)

	defer func() { //nolint:contextcheck // intentionally use Background for graceful shutdown after ctx cancellation
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	authURL := f.buildAuthURL(port, state, codeChallenge)

	fmt.Fprintln(f.writer, "Opening browser to authenticate...")
	fmt.Fprintf(f.writer, "If browser doesn't open, visit:\n  %s\n\n", authURL)

	fmt.Fprintf(f.writer, "Verification code: %s\n", verificationCode(codeChallenge))
	fmt.Fprintln(f.writer, "Check that this code matches what is shown in the browser before approving.")
	fmt.Fprintln(f.writer)

	if opened, err := deeplink.OpenWithStatus(authURL); err != nil {
		fmt.Fprintln(f.writer, "(Could not open browser automatically)")
	} else if !opened {
		fmt.Fprintln(f.writer, "(Browser launch skipped in agent mode — open the URL above manually)")
	}

	// Over SSH the browser cannot reach the callback address. Accept a pasted
	// redirect URL alongside the callback so the user never has to restart.
	paste := startPasteWatcher(f.writer, port)
	defer paste.Close()
	if paste == nil {
		printRemoteSessionHint(f.writer, port, "gcx login --oauth-manual")
		fmt.Fprintln(f.writer, "Waiting for authentication...")
	}

	announcedIgnored := false
	for {
		select {
		case result := <-resultCh:
			return result, nil
		case err := <-errCh:
			return nil, err
		case pasted := <-paste.Input():
			if out := f.resolvePaste(ctx, arb, paste, pasted, state, codeVerifier); out.done {
				return out.result, out.err
			}
		case <-ignored:
			if !announcedIgnored {
				announcedIgnored = true
				fmt.Fprintln(f.writer, ignoredCallbackNotice)
			}
		case <-arb.expired():
			return nil, errCallbackTimeout(f.acquireTimeout)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// resolvePaste runs one delivered redirect URL through the ownership gate, the
// arbiter, and — when it owns the flow — the token exchange. done reports
// whether the flow finished; when it is false the watcher has already been told
// why, and the loop keeps waiting.
//
// It lives outside the select so both the wiring and every branch of it can be
// tested without a live browser: startPasteWatcher needs a real terminal and a
// non-agent session, which no test can supply without opening a browser window.
func (f *Flow) resolvePaste(ctx context.Context, arb *callbackArbiter, paste *pasteWatcher, pasted pastedInput, state, codeVerifier string) pasteOutcome[Result] {
	if pasted.Err != nil {
		paste.Reject(pasted.Err)
		return pasteKeepWaiting[Result]()
	}

	switch claimPastedCallback(arb, pasted.Values, state) {
	case pasteForeign:
		paste.Reject(errManualForeignState)
		return pasteKeepWaiting[Result]()
	case pasteSuperseded:
		fmt.Fprintln(f.writer, pasteSupersededNotice)
		return pasteKeepWaiting[Result]()
	case pasteExpired:
		// The expiry case ends the flow on the next turn of the loop, with the
		// timeout error. Prompting again here would be a lie.
		return pasteKeepWaiting[Result]()
	case pasteClaimed:
	}

	result, cerr := handleCallbackParams(ctx, pasted.Values, state, codeVerifier)
	switch {
	case cerr == nil:
		arb.settle()
		fmt.Fprintln(f.writer, manualCallbackHygieneNotice)
		return pasteOutcome[Result]{done: true, result: result}
	case cerr.spent:
		// The code reached the token endpoint. Releasing the flow would let the
		// browser callback — carrying that same code — exchange it a second
		// time, so this ends the login. Another paste could not help anyway:
		// the code is gone.
		arb.settle()
		return pasteOutcome[Result]{done: true, err: cerr.err}
	case !arb.release():
		// The deadline passed while this attempt was running. Do not prompt for
		// input that can no longer be accepted.
		return pasteKeepWaiting[Result]()
	default:
		paste.Reject(pasteRejection(cerr.err))
		return pasteKeepWaiting[Result]()
	}
}

// buildAuthURL renders the plugin consent URL for the given callback port.
func (f *Flow) buildAuthURL(port int, state, codeChallenge string) string {
	authEndpoint := strings.TrimSuffix(f.endpoint, "/")
	if authEndpoint == "" {
		authEndpoint = "https://grafana.com/launch"
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

func (f *Flow) startCallbackServer(ctx context.Context, listener net.Listener, expectedState, codeVerifier string, arb *callbackArbiter, resultCh chan<- *Result, errCh chan<- error, ignored chan<- struct{}) *http.Server {
	return newCallbackServer(listener, expectedState, arb, errCh, ignored, func(w http.ResponseWriter, r *http.Request) {
		result, cerr := handleCallbackParams(ctx, r.URL.Query(), expectedState, codeVerifier)
		if cerr != nil {
			errCh <- cerr.err
			renderErrorPage(w, cerr.page)
			return
		}

		resultCh <- result
		renderSuccessPage(w)
	})
}

// newCallbackServer binds the /callback handler to listener and starts serving
// in a goroutine. Serve errors are reported on errCh.
//
// The handler answers every request, but only a request that belongs to this
// flow may consume it. Ordering matters and is the whole of the fix for #1147:
// the guard used to be claimed on arrival, ahead of the state check, so any
// unrelated request aborted the login and left the real callback to be answered
// with 410 Gone.
//
//	non-GET           -> 405 Allow: GET      no claim, flow untouched
//	state not ours    -> 400 + browser page  no claim, flow untouched
//	already claimed   -> 410 Gone            replay stays rejected
//	deadline passed   -> 410 Gone            flow already gave up
//	ours and first    -> handle(), terminal
func newCallbackServer(listener net.Listener, expectedState string, arb *callbackArbiter, errCh chan<- error, ignored chan<- struct{}, handle http.HandlerFunc) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// An explicit check, not a "GET /callback" ServeMux pattern: that
		// pattern also matches HEAD, which would let a HEAD request run the
		// token exchange as a side effect.
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			notifyIgnored(ignored)
			return
		}

		// Ownership decides who may consume the flow. Anything else is
		// answered and forgotten: a loopback listener is reachable by every
		// local process, and in unified login the first OAuth leg's callback
		// commonly lands on the second leg's port.
		//
		// A notice page, not the error page: for the user standing in front of
		// the browser this is a stale tab, not a failed login, and the sign-in
		// they care about is still running in their terminal.
		if !callbackBelongsToFlow(r.URL.Query(), expectedState) {
			renderNoticePage(w, foreignCallbackPage)
			notifyIgnored(ignored)
			return
		}

		switch arb.claim() {
		case claimTaken:
			http.Error(w, "Authentication already processed", http.StatusGone)
			return
		case claimExpired:
			http.Error(w, "This sign-in timed out. Run the command again.", http.StatusGone)
			return
		case claimGranted:
		}

		// handle owns the flow from here, so it must always produce an
		// outcome. net/http recovers a panic per connection, which would
		// otherwise leave nothing on resultCh or errCh and — because a
		// granted claim disarms the deadline — hang the flow forever.
		defer func() {
			if rec := recover(); rec != nil {
				// The panic value is not reported: it can carry request data.
				select {
				case errCh <- errors.New("callback handler failed"):
				default:
				}
			}
			arb.settle()
		}()
		handle(w, r)
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

// notifyIgnored tells the flow that a request was answered and discarded, so
// the flow's own goroutine can say so once. The send never blocks and never
// carries request data: the handler runs on the server's goroutine and must not
// touch the writer the flow owns, nor wedge if nobody is listening.
func notifyIgnored(ignored chan<- struct{}) {
	if ignored == nil {
		return
	}
	select {
	case ignored <- struct{}{}:
	default:
	}
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
		return nil, fmt.Errorf("oauth token exchange failed: status %d from %s", resp.StatusCode, req.URL.Path)
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

// ValidateCallbackPort reports whether port is usable as a fixed OAuth callback
// port. Zero means "pick one automatically" from 54321-54399.
//
// This is only the numeric rule. It lives here so that every command exposing a
// callback-port flag accepts exactly the same values; each command layer adds
// its own guidance to the error it returns.
func ValidateCallbackPort(port int) error {
	if port < 0 || port > 65535 {
		return fmt.Errorf("callback port must be between 1 and 65535 (or 0 to auto-pick); got %d", port)
	}
	return nil
}

func listenOnCallbackPort(ctx context.Context, bindAddress string, fixedPort int) (net.Listener, int, error) {
	if err := ValidateCallbackPort(fixedPort); err != nil {
		return nil, 0, err
	}
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

// renderNoticePage answers a request that gcx deliberately did nothing with.
// It is not renderErrorPage: that template says "Authentication Failed" and
// tells the reader to try again, which is wrong and alarming for a stale tab
// that did not affect any login.
func renderNoticePage(w http.ResponseWriter, message string) {
	tmpl := template.Must(template.ParseFS(templateFS, "templates/notice.html"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	data := struct{ Message string }{Message: message}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
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
