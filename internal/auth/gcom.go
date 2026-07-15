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
	Info        struct {
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
}

// GCOMFlow manages a direct GCOM OAuth2 PKCE authentication flow.
type GCOMFlow struct {
	opts   GCOMOptions
	writer io.Writer
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
	return &GCOMFlow{opts: opts, writer: w}
}

// Run executes the GCOM OAuth2 PKCE flow.
func (f *GCOMFlow) Run(ctx context.Context) (*GCOMResult, error) {
	if err := validateGCOMURL(f.opts.GCOMURL); err != nil {
		return nil, fmt.Errorf("invalid GCOM URL: %w", err)
	}

	listener, port, err := listenOnCallbackPort(ctx, "127.0.0.1", 0)
	if err != nil {
		return nil, fmt.Errorf("no available port: %w", err)
	}

	state, err := generateState()
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to generate PKCE code verifier: %w", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)

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

	gcomURL := strings.TrimSuffix(f.opts.GCOMURL, "/")
	scope := strings.Join(f.opts.Scopes, " ")

	authURL := fmt.Sprintf("%s/oauth2/authorize?client_id=%s&redirect_uri=%s&scope=%s&code_challenge=%s&code_challenge_method=S256&state=%s&response_type=code",
		gcomURL,
		url.QueryEscape(f.opts.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(scope),
		url.QueryEscape(codeChallenge),
		url.QueryEscape(state),
	)

	fmt.Fprintln(f.writer, "Opening browser to authenticate with Grafana Cloud...")
	fmt.Fprintf(f.writer, "If browser doesn't open, visit:\n  %s\n\n", authURL)

	if err := deeplink.Open(authURL); err != nil {
		fmt.Fprintln(f.writer, "(Could not open browser automatically)")
	}

	fmt.Fprintln(f.writer, "Waiting for authentication...")

	select {
	case result := <-resultCh:
		return result, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *GCOMFlow) startGCOMCallbackServer(ctx context.Context, listener net.Listener, expectedState, codeVerifier, redirectURI string, resultCh chan<- *GCOMResult, errCh chan<- error) *http.Server {
	return newCallbackServer(listener, errCh, func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		if state != expectedState {
			errCh <- errors.New("invalid state - possible CSRF attack")
			renderErrorPage(w, "Invalid state parameter")
			return
		}

		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("authentication denied: %s", StripControlChars(errMsg))
			renderErrorPage(w, StripControlChars(errMsg))
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- errors.New("no authorization code received")
			renderErrorPage(w, "No authorization code received")
			return
		}

		result, err := f.exchangeGCOMToken(ctx, code, codeVerifier, redirectURI)
		if err != nil {
			errCh <- fmt.Errorf("token exchange failed: %w", err)
			renderErrorPage(w, "Token exchange failed")
			return
		}

		resultCh <- result
		renderSuccessPage(w)
	})
}

type gcomTokenResponse struct {
	AccessToken string `json:"access_token"`
	Scope       string `json:"scope"`
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

	return &GCOMResult{
		AccessToken: tokenResp.AccessToken,
		Scope:       tokenResp.Scope,
		Info:        tokenResp.Info,
	}, nil
}
