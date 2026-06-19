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
	"sync"
	"time"
)

// GCOMResult contains the result of a GCOM OAuth2 PKCE authentication flow.
type GCOMResult struct {
	AccessToken string
	Scope       string
	ExpiresIn   int
	UserID      int
	Info        struct {
		Name  string `json:"name"`
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

	// Port specifies a fixed port for the callback server. 0 = auto.
	Port int

	// BindAddress for the callback server. Defaults to "127.0.0.1".
	BindAddress string

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
	if opts.BindAddress == "" {
		opts.BindAddress = "127.0.0.1"
	}
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
	listener, port, err := listenOnCallbackPort(ctx, f.opts.BindAddress, f.opts.Port)
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

	if err := openBrowser(ctx, authURL); err != nil {
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
	var once sync.Once

	mux := http.NewServeMux()

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		handled := false
		once.Do(func() {
			handled = true
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
		if !handled {
			http.Error(w, "Authentication already processed", http.StatusGone)
		}
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

type gcomTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
	UID         int    `json:"uid"`
	Info        struct {
		Name  string `json:"name"`
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange returned status %d: %s", resp.StatusCode, string(respBody))
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
		ExpiresIn:   tokenResp.ExpiresIn,
		UserID:      tokenResp.UID,
		Info:        tokenResp.Info,
	}, nil
}
