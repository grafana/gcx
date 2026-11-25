package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/grafana/gcx/internal/httputils"
)

// DefaultOIDCScopes returns the scopes requested from a generic OIDC provider.
// openid yields the ID token that Grafana validates, profile and email carry
// the claims Grafana maps to its user, and offline_access yields the refresh
// token that keeps the login usable after the ID token expires. A fresh slice
// is returned on each call.
func DefaultOIDCScopes() []string {
	return []string{"openid", "profile", "email", "offline_access"}
}

// OIDCOptions configures a generic OIDC authorization code + PKCE login.
type OIDCOptions struct {
	// Issuer is the OIDC issuer URL; discovery reads
	// <Issuer>/.well-known/openid-configuration.
	Issuer string

	// ClientID is the public client registered at the identity provider.
	ClientID string

	// Scopes defaults to DefaultOIDCScopes.
	Scopes []string

	// Writer for user-facing messages. Defaults to os.Stderr.
	Writer io.Writer

	// Reader supplies the pasted redirect URL. Defaults to os.Stdin.
	Reader io.Reader

	// Port fixes the local callback port. Zero picks an ephemeral port.
	Port int

	// Manual completes the flow without a callback server: gcx prints the
	// login URL and reads the redirect URL copied from the browser.
	Manual bool
}

// OIDCResult is the outcome of a successful OIDC login.
type OIDCResult struct {
	// IDToken is sent to Grafana as the bearer token.
	IDToken      string
	RefreshToken string
	// ExpiresAt is the ID token expiry in RFC3339 format.
	ExpiresAt string
	Email     string
}

// OIDCFlow runs an OIDC authorization code + PKCE login in the browser.
type OIDCFlow struct {
	opts   OIDCOptions
	writer io.Writer
	reader io.Reader
}

// NewOIDCFlow creates a new OIDC login flow.
func NewOIDCFlow(opts OIDCOptions) *OIDCFlow {
	if len(opts.Scopes) == 0 {
		opts.Scopes = DefaultOIDCScopes()
	}
	w := opts.Writer
	if w == nil {
		w = os.Stderr
	}
	r := opts.Reader
	if r == nil {
		r = os.Stdin
	}
	return &OIDCFlow{opts: opts, writer: w, reader: r}
}

// Run executes the OIDC login.
func (f *OIDCFlow) Run(ctx context.Context) (*OIDCResult, error) {
	if strings.TrimSpace(f.opts.ClientID) == "" {
		return nil, errors.New("OIDC client ID is required")
	}
	provider, err := DiscoverOIDC(ctx, f.opts.Issuer)
	if err != nil {
		return nil, err
	}
	if f.opts.Manual {
		return f.runManual(ctx, provider)
	}
	return f.runWithCallbackServer(ctx, provider)
}

func (f *OIDCFlow) runManual(ctx context.Context, provider *OIDCProvider) (*OIDCResult, error) {
	state, codeVerifier, codeChallenge, err := newFlowSecrets()
	if err != nil {
		return nil, err
	}
	nonce, err := generateNonce()
	if err != nil {
		return nil, err
	}
	redirectURI := oidcRedirectURI(manualCallbackPort)
	authURL, err := f.buildAuthURL(provider, redirectURI, state, nonce, codeChallenge)
	if err != nil {
		return nil, err
	}
	return runManualPaste(ctx, f.writer, f.reader, authURL, "", "",
		func(q url.Values) (*OIDCResult, *callbackError) {
			return f.handleCallbackParams(ctx, provider, q, state, nonce, codeVerifier, redirectURI, nil)
		})
}

func (f *OIDCFlow) runWithCallbackServer(ctx context.Context, provider *OIDCProvider) (*OIDCResult, error) {
	listener, port, err := listenOnCallbackPort(ctx, "127.0.0.1", f.opts.Port)
	if err != nil {
		return nil, err
	}

	state, codeVerifier, codeChallenge, err := newFlowSecrets()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	nonce, err := generateNonce()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	redirectURI := oidcRedirectURI(port)
	authURL, err := f.buildAuthURL(provider, redirectURI, state, nonce, codeChallenge)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	resultCh := make(chan *OIDCResult, 1)
	errCh := make(chan error, 1)
	guard := &exchangeGuard{}
	server := f.startCallbackServer(ctx, provider, listener, state, nonce, codeVerifier, redirectURI, guard, resultCh, errCh)

	//nolint:contextcheck // shutdown must not inherit the (possibly cancelled) request context
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Fprintln(f.writer, "Opening browser to authenticate with your identity provider...")
	fmt.Fprintf(f.writer, "If browser doesn't open, visit:\n  %s\n\n", authURL)

	if opened, err := openBrowser(authURL); err != nil {
		fmt.Fprintln(f.writer, "(Could not open browser automatically)")
	} else if !opened {
		fmt.Fprintln(f.writer, "(Browser launch skipped in agent mode; open the URL above manually)")
	}

	paste := startPasteWatcher(f.writer, port)
	defer paste.Close()
	if paste == nil {
		printRemoteSessionHint(f.writer, port, "gcx login --web --oauth-manual")
		fmt.Fprintln(f.writer, "Waiting for authentication...")
	}

	return awaitCallbackOrPaste(ctx, f.writer, paste, nil, resultCh, errCh,
		func(q url.Values) (*OIDCResult, *callbackError) {
			return f.handleCallbackParams(ctx, provider, q, state, nonce, codeVerifier, redirectURI, guard)
		})
}

// oidcRedirectURI uses the host name localhost: identity providers such as
// Entra ID accept any port for a registered http://localhost/callback redirect.
// The listener binds 127.0.0.1.
func oidcRedirectURI(port int) string {
	return fmt.Sprintf("http://localhost:%d/callback", port)
}

func (f *OIDCFlow) buildAuthURL(provider *OIDCProvider, redirectURI, state, nonce, codeChallenge string) (string, error) {
	u, err := url.Parse(provider.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("invalid authorization endpoint: %w", err)
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", f.opts.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", strings.Join(f.opts.Scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func (f *OIDCFlow) startCallbackServer(ctx context.Context, provider *OIDCProvider, listener net.Listener, state, nonce, codeVerifier, redirectURI string, guard *exchangeGuard, resultCh chan<- *OIDCResult, errCh chan<- error) *http.Server {
	return newCallbackServer(listener, state, errCh, func(w http.ResponseWriter, r *http.Request) bool {
		result, cerr := f.handleCallbackParams(ctx, provider, r.URL.Query(), state, nonce, codeVerifier, redirectURI, guard)
		if cerr != nil {
			return answerCallbackError(w, f.writer, cerr, errCh)
		}
		resultCh <- result
		renderSuccessPage(w, nil)
		return true
	})
}

func (f *OIDCFlow) handleCallbackParams(ctx context.Context, provider *OIDCProvider, q url.Values, state, nonce, codeVerifier, redirectURI string, guard *exchangeGuard) (*OIDCResult, *callbackError) {
	code, cerr := checkCallbackBasics(q, state)
	if cerr != nil {
		return nil, cerr
	}
	if !guard.claim() {
		return nil, &callbackError{err: errExchangeClaimed, page: "The login already completed in the terminal."}
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {f.opts.ClientID},
		"code":          {code},
		"code_verifier": {codeVerifier},
		"redirect_uri":  {redirectURI},
	}
	tokens, err := oidcTokenRequest(ctx, provider.TokenEndpoint, form)
	if err != nil {
		return nil, &callbackError{err: fmt.Errorf("token exchange failed: %w", err), page: "Token exchange failed"}
	}
	if tokens.IDToken == "" {
		return nil, &callbackError{err: errors.New("token response missing id_token"), page: "Token exchange failed"}
	}
	claims, err := checkIDToken(tokens.IDToken, provider.Issuer, f.opts.ClientID, nonce)
	if err != nil {
		return nil, &callbackError{err: fmt.Errorf("invalid ID token: %w", err), page: "Invalid ID token"}
	}

	return &OIDCResult{
		IDToken:      tokens.IDToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    claims.expiresAt(),
		Email:        claims.Email,
	}, nil
}

// OIDCProvider is the subset of the OIDC discovery document gcx uses.
type OIDCProvider struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

// DiscoverOIDC reads the OIDC discovery document of issuer.
func DiscoverOIDC(ctx context.Context, issuer string) (*OIDCProvider, error) {
	return discoverOIDC(ctx, issuer, httputils.NewClient(httputils.ClientOpts{Timeout: 10 * time.Second}))
}

func discoverOIDC(ctx context.Context, issuer string, client *http.Client) (*OIDCProvider, error) {
	issuer = strings.TrimSpace(issuer)
	if err := validateOIDCURL(issuer); err != nil {
		return nil, fmt.Errorf("invalid OIDC issuer: %w", err)
	}
	discoveryURL := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC discovery request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OIDC discovery failed: status %d from %s", resp.StatusCode, discoveryURL)
	}

	var provider OIDCProvider
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&provider); err != nil {
		return nil, fmt.Errorf("failed to parse OIDC discovery document: %w", err)
	}
	if strings.TrimSuffix(provider.Issuer, "/") != strings.TrimSuffix(issuer, "/") {
		return nil, fmt.Errorf("OIDC discovery issuer %q does not match configured issuer %q", provider.Issuer, issuer)
	}
	if err := validateOIDCURL(provider.AuthorizationEndpoint); err != nil {
		return nil, fmt.Errorf("invalid OIDC authorization endpoint: %w", err)
	}
	if err := validateOIDCURL(provider.TokenEndpoint); err != nil {
		return nil, fmt.Errorf("invalid OIDC token endpoint: %w", err)
	}
	return &provider, nil
}

// oidcProviders caches discovery documents for token refresh, so a
// long-running process does not fetch the document before every refresh.
var oidcProviders sync.Map //nolint:gochecknoglobals // process-wide cache of immutable discovery results

func cachedOIDCProvider(ctx context.Context, issuer string) (*OIDCProvider, error) {
	if cached, ok := oidcProviders.Load(issuer); ok {
		provider, _ := cached.(*OIDCProvider)
		return provider, nil
	}
	provider, err := DiscoverOIDC(ctx, issuer)
	if err != nil {
		return nil, err
	}
	oidcProviders.Store(issuer, provider)
	return provider, nil
}

// NormalizeOIDCScopes returns scopes without duplicates, always including
// openid (for the ID token) and offline_access (for the refresh token).
func NormalizeOIDCScopes(scopes []string) []string {
	result := []string{"openid", "offline_access"}
	for _, scope := range scopes {
		if scope != "" && !slices.Contains(result, scope) {
			result = append(result, scope)
		}
	}
	return result
}

// OIDCRefresh exchanges refreshToken for a new ID token. scopes should be the
// scopes of the original login: refresh scopes must not exceed them, and
// sending them keeps the refreshed tokens equivalent to the login's. Providers
// that do not rotate refresh tokens omit refresh_token from the response; the
// current one is then kept.
func OIDCRefresh(ctx context.Context, issuer, clientID string, scopes []string, refreshToken string) (RefreshResult, error) {
	provider, err := cachedOIDCProvider(ctx, issuer)
	if err != nil {
		return RefreshResult{}, err
	}
	if len(scopes) == 0 {
		scopes = DefaultOIDCScopes()
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
		"scope":         {strings.Join(scopes, " ")},
	}
	tokens, err := oidcTokenRequest(ctx, provider.TokenEndpoint, form)
	if err != nil {
		if errors.Is(err, errOIDCInvalidGrant) {
			return RefreshResult{}, fmt.Errorf("oidc refresh failed: %w", ErrRefreshTokenExpired)
		}
		return RefreshResult{}, fmt.Errorf("oidc refresh failed: %w", err)
	}
	if tokens.IDToken == "" {
		return RefreshResult{}, fmt.Errorf("%w: missing id_token", ErrInvalidRefreshResponse)
	}
	claims, err := checkIDToken(tokens.IDToken, provider.Issuer, clientID, "")
	if err != nil {
		return RefreshResult{}, fmt.Errorf("%w: %w", ErrInvalidRefreshResponse, err)
	}
	newRefreshToken := tokens.RefreshToken
	if newRefreshToken == "" {
		newRefreshToken = refreshToken
	}
	return RefreshResult{
		Token:        tokens.IDToken,
		RefreshToken: newRefreshToken,
		ExpiresAt:    claims.expiresAt(),
	}, nil
}

var errOIDCInvalidGrant = errors.New("invalid_grant")

type oidcTokenResponse struct {
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
}

// oidcTokenRequest posts a form to the token endpoint. Retries and redirects
// are disabled: an authorization code or rotating refresh token must not be
// presented twice or to another host. Payload logging is off because the form
// carries secrets.
func oidcTokenRequest(ctx context.Context, tokenEndpoint string, form url.Values) (*oidcTokenResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := httputils.NewClient(httputils.ClientOpts{
		DisableRetry: true,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not allowed for the OIDC token endpoint")
		},
	})
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read token response: %w", err)
	}
	var tokens oidcTokenResponse
	parseErr := json.Unmarshal(body, &tokens)
	if resp.StatusCode != http.StatusOK {
		// The body is not included: it may echo request data.
		if parseErr == nil && tokens.Error == "invalid_grant" {
			return nil, fmt.Errorf("status %d: %w", resp.StatusCode, errOIDCInvalidGrant)
		}
		if parseErr == nil && tokens.Error != "" {
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, StripControlChars(tokens.Error))
		}
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if parseErr != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", parseErr)
	}
	return &tokens, nil
}

type idTokenClaims struct {
	Issuer   string   `json:"iss"`
	Audience audience `json:"aud"`
	Expiry   int64    `json:"exp"`
	Nonce    string   `json:"nonce"`
	Email    string   `json:"email"`
}

func (c idTokenClaims) expiresAt() string {
	if c.Expiry == 0 {
		return ""
	}
	return time.Unix(c.Expiry, 0).UTC().Format(time.RFC3339)
}

// audience accepts the string and array forms of the aud claim.
type audience []string

func (a *audience) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*a = audience{single}
		return nil
	}
	var multiple []string
	if err := json.Unmarshal(data, &multiple); err != nil {
		return err
	}
	*a = multiple
	return nil
}

// checkIDToken checks the claims of an ID token received directly from the
// token endpoint over TLS. OIDC Core 3.1.3.7 allows the TLS server validation
// to stand in for the signature check in this case; Grafana verifies the
// signature on every request. An empty nonce skips the nonce check (refresh).
func checkIDToken(idToken, issuer, clientID, nonce string) (*idTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("malformed JWT payload: %w", err)
	}
	var claims idTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("malformed JWT claims: %w", err)
	}
	if strings.TrimSuffix(claims.Issuer, "/") != strings.TrimSuffix(issuer, "/") {
		return nil, fmt.Errorf("issuer %q does not match %q", claims.Issuer, issuer)
	}
	if !slices.Contains(claims.Audience, clientID) {
		return nil, errors.New("audience does not contain the client ID")
	}
	if nonce != "" && claims.Nonce != nonce {
		return nil, errors.New("nonce mismatch")
	}
	if claims.Expiry != 0 && time.Now().After(time.Unix(claims.Expiry, 0)) {
		return nil, errors.New("token expired")
	}
	return &claims, nil
}

// validateOIDCURL requires HTTPS, except for loopback addresses.
func validateOIDCURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Host == "" {
		return errors.New("URL has no host")
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return nil
	}
	if u.Scheme != "https" {
		return fmt.Errorf("URL must use HTTPS, got %q", u.Scheme)
	}
	return nil
}

func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}
