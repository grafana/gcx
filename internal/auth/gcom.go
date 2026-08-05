package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/deeplink"
	"github.com/grafana/gcx/internal/httputils"
)

// DefaultGCOMClientID is the OAuth2 client ID registered in GCOM for gcx.
const DefaultGCOMClientID = "gcx"

// DefaultGCOMScopes returns the grafana.com API scopes gcx needs across all
// commands: stacks (discovery + management), the signal write scopes for
// minting the Synthetic Monitoring token (metrics/logs/traces:write), and
// Fleet Management. Both `gcx cloud login` and the `gcx login` cloud followup
// request this set. A fresh slice is returned on each call so callers (e.g. a
// Cobra flag default) can mutate their copy without affecting others.
func DefaultGCOMScopes() []string {
	return []string{
		"stacks:read", "stacks:write", "stacks:delete",
		"metrics:write",
		"logs:write",
		"traces:write",
		"fleet-management:read", "fleet-management:write",
	}
}

// GCOMResult contains the result of a GCOM OAuth2 PKCE authentication flow.
type GCOMResult struct {
	AccessToken string
	Scope       string
	// ExpiresAt is the access token expiration time in RFC3339 format,
	// derived from the token response's expires_in. Empty when the server
	// does not report a lifetime.
	ExpiresAt string
	Info      struct {
		Email string `json:"email"`
		Login string `json:"login"`
	}
}

// GCOMOptions configures the GCOM OAuth2 PKCE flow.
type GCOMOptions struct {
	// ClientID is the OAuth2 client ID registered in GCOM.
	ClientID string

	// GCOMURL is the base URL of the GCOM API (e.g. "https://grafana.com").
	GCOMURL string

	// Scopes is the list of OAuth2 scopes to request.
	Scopes []string

	// Writer for user-facing messages. Defaults to os.Stderr.
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

// GCOMFlow manages a direct GCOM OAuth2 PKCE authentication flow.
type GCOMFlow struct {
	opts   GCOMOptions
	writer io.Writer
	reader io.Reader
}

// NewGCOMFlow creates a new GCOM OAuth2 PKCE flow.
func NewGCOMFlow(opts GCOMOptions) *GCOMFlow {
	if opts.GCOMURL == "" {
		opts.GCOMURL = "https://grafana.com"
	}
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	r := opts.Reader
	if r == nil {
		r = os.Stdin
	}
	return &GCOMFlow{opts: opts, writer: w, reader: r}
}

// Run executes the GCOM OAuth2 PKCE flow.
func (f *GCOMFlow) Run(ctx context.Context) (*GCOMResult, error) {
	if err := validateGCOMURL(f.opts.GCOMURL); err != nil {
		return nil, fmt.Errorf("invalid GCOM URL: %w", err)
	}

	if f.opts.Manual {
		return f.runManual(ctx)
	}
	return f.runWithCallbackServer(ctx)
}

// runManual completes the flow without a callback server. The browser cannot
// reach the callback address, so the user copies the redirect URL out of the
// address bar and pastes it here.
func (f *GCOMFlow) runManual(ctx context.Context) (*GCOMResult, error) {
	state, codeVerifier, codeChallenge, err := newFlowSecrets()
	if err != nil {
		return nil, err
	}

	// The token exchange must send this exact string, so build it once and
	// share it between the authorize URL and the exchange.
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", manualCallbackPort)

	authURL := f.buildAuthURL(redirectURI, state, codeChallenge)
	printManualInstructions(f.writer, authURL, "")

	line, err := readLineContext(ctx, f.reader)
	if err != nil {
		return nil, err
	}

	values, err := ParseCallbackInput(line)
	if err != nil {
		return nil, fmt.Errorf("cannot read the redirect URL: %w", err)
	}

	result, cerr := f.handleGCOMCallbackParams(ctx, values, state, codeVerifier, redirectURI)
	if cerr != nil {
		return nil, pasteRejection(cerr.err)
	}

	fmt.Fprintln(f.writer, manualCallbackHygieneNotice)
	return result, nil
}

func (f *GCOMFlow) runWithCallbackServer(ctx context.Context) (*GCOMResult, error) {
	listener, port, err := listenOnCallbackPort(ctx, "127.0.0.1", 0)
	if err != nil {
		return nil, fmt.Errorf("no available port: %w", err)
	}

	state, codeVerifier, codeChallenge, err := newFlowSecrets()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	resultCh := make(chan *GCOMResult, 1)
	errCh := make(chan error, 1)
	server := f.startGCOMCallbackServer(ctx, listener, state, codeVerifier, redirectURI, resultCh, errCh)

	// A fresh context is intentional: the request context may already be
	// cancelled by the time we shut down, and graceful shutdown needs its own
	// timeout.
	//nolint:contextcheck // shutdown must not inherit the (possibly cancelled) request context
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	authURL := f.buildAuthURL(redirectURI, state, codeChallenge)

	fmt.Fprintln(f.writer, "Opening browser to authenticate with Grafana Cloud...")
	fmt.Fprintf(f.writer, "If browser doesn't open, visit:\n  %s\n\n", authURL)

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
		printRemoteSessionHint(f.writer, port, "gcx cloud login --oauth-manual")
		fmt.Fprintln(f.writer, "Waiting for authentication...")
	}

	for {
		select {
		case result := <-resultCh:
			return result, nil
		case err := <-errCh:
			return nil, err
		case values := <-paste.Values():
			result, cerr := f.handleGCOMCallbackParams(ctx, values, state, codeVerifier, redirectURI)
			if cerr != nil {
				paste.Reject(pasteRejection(cerr.err))
				continue
			}
			fmt.Fprintln(f.writer, manualCallbackHygieneNotice)
			return result, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// buildAuthURL renders the GCOM authorize URL. redirectURI must be the exact
// string that the token exchange later sends.
func (f *GCOMFlow) buildAuthURL(redirectURI, state, codeChallenge string) string {
	gcomURL := strings.TrimSuffix(f.opts.GCOMURL, "/")
	scope := strings.Join(f.opts.Scopes, " ")

	return fmt.Sprintf("%s/oauth2/authorize?client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s&response_type=code",
		gcomURL,
		url.QueryEscape(f.opts.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(scope),
		url.QueryEscape(codeChallenge),
		url.QueryEscape(state),
	)
}

func (f *GCOMFlow) startGCOMCallbackServer(ctx context.Context, listener net.Listener, expectedState, codeVerifier, redirectURI string, resultCh chan<- *GCOMResult, errCh chan<- error) *http.Server {
	return newCallbackServer(listener, errCh, func(w http.ResponseWriter, r *http.Request) {
		result, cerr := f.handleGCOMCallbackParams(ctx, r.URL.Query(), expectedState, codeVerifier, redirectURI)
		if cerr != nil {
			errCh <- cerr.err
			renderErrorPage(w, cerr.page)
			return
		}

		resultCh <- result
		renderSuccessPage(w)
	})
}

type gcomTokenResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
	ExpiresIn   int64  `json:"expires_in"`
	Info        struct {
		Email string `json:"email"`
		Login string `json:"login"`
	} `json:"info"`
}

func (f *GCOMFlow) exchangeGCOMToken(ctx context.Context, code, codeVerifier, redirectURI string) (*GCOMResult, error) {
	gcomURL := strings.TrimSuffix(f.opts.GCOMURL, "/")
	tokenURL := gcomURL + "/api/oauth2/token"

	body, err := json.Marshal(map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     f.opts.ClientID,
		"code":          code,
		"code_verifier": codeVerifier,
		"redirect_uri":  redirectURI,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal token request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// NewClient (not NewDefaultClient): NewDefaultClient logs payloads from ctx,
	// which would dump the OAuth code/code_verifier secrets.
	client := httputils.NewClient(httputils.ClientOpts{
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			redirectEndpoint := req.URL.Scheme + "://" + req.URL.Host
			if err := ValidateEndpointURL(redirectEndpoint); err != nil {
				return fmt.Errorf("redirect to untrusted URL blocked: %w", err)
			}
			return nil
		},
	})

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange returned status %d", resp.StatusCode)
	}

	var tokenResp gcomTokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, errors.New("token response missing access_token")
	}

	result := &GCOMResult{
		AccessToken: tokenResp.AccessToken,
		Scope:       tokenResp.Scope,
		Info:        tokenResp.Info,
	}
	if tokenResp.ExpiresIn > 0 {
		result.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	return result, nil
}
