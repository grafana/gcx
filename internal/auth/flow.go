// Package auth implements the browser-based OAuth PKCE authentication flow for grafanactl.
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
	"html"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

var (
	successTemplate = template.Must(template.ParseFS(templateFS, "templates/success.html"))
	errorTemplate   = template.Must(template.ParseFS(templateFS, "templates/error.html"))
)

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
}

// DefaultScopes are the scopes requested by grafanactl.
var DefaultScopes = []string{"grafana-api:read", "grafana-api:write", "grafana-api:delete"}

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
}

// Flow manages the browser-based authentication process.
type Flow struct {
	endpoint string
	opts     Options
}

// NewFlow creates a new authentication flow for the given Grafana endpoint.
func NewFlow(endpoint string, opts Options) *Flow {
	if opts.BindAddress == "" {
		opts.BindAddress = "127.0.0.1"
	}
	if len(opts.Scopes) == 0 {
		opts.Scopes = DefaultScopes
	}
	return &Flow{endpoint: endpoint, opts: opts}
}

// Run executes the authentication flow.
func (f *Flow) Run(ctx context.Context) (*Result, error) {
	port := f.opts.Port
	if port == 0 {
		var err error
		port, err = findAvailablePort(f.opts.BindAddress)
		if err != nil {
			return nil, fmt.Errorf("no available port: %w", err)
		}
	}

	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE code verifier: %w", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)

	resultCh := make(chan *Result, 1)
	errCh := make(chan error, 1)
	server := f.startCallbackServer(ctx, f.opts.BindAddress, port, state, codeVerifier, resultCh, errCh)

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	authURL := fmt.Sprintf("%s/a/grafana-assistant-app/cli/auth?callback_port=%d&state=%s&code_challenge=%s&code_challenge_method=S256",
		f.endpoint, port, url.QueryEscape(state), url.QueryEscape(codeChallenge))

	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		authURL += "&device_name=" + url.QueryEscape(hostname)
	}

	if len(f.opts.Scopes) > 0 {
		authURL += "&scopes=" + url.QueryEscape(strings.Join(f.opts.Scopes, ","))
	}

	fmt.Println("Opening browser to authenticate...")
	fmt.Printf("If browser doesn't open, visit:\n  %s\n\n", authURL)

	if err := openBrowser(authURL); err != nil {
		fmt.Println("(Could not open browser automatically)")
	}

	fmt.Println("Waiting for authentication...")

	select {
	case result := <-resultCh:
		return result, nil
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *Flow) startCallbackServer(ctx context.Context, bindAddress string, port int, expectedState, codeVerifier string, resultCh chan<- *Result, errCh chan<- error) *http.Server {
	var once sync.Once

	mux := http.NewServeMux()

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			state := r.URL.Query().Get("state")
			if state != expectedState {
				errCh <- errors.New("invalid state - possible CSRF attack")
				renderErrorPage(w, "Invalid state parameter")
				return
			}

			if errMsg := r.URL.Query().Get("error"); errMsg != "" {
				errCh <- fmt.Errorf("authentication denied: %s", errMsg)
				renderErrorPage(w, errMsg)
				return
			}

			code := r.URL.Query().Get("code")
			if code == "" {
				errCh <- errors.New("no authorization code received")
				renderErrorPage(w, "No authorization code received")
				return
			}

			endpoint := r.URL.Query().Get("endpoint")
			if endpoint == "" {
				errCh <- errors.New("no API endpoint received")
				renderErrorPage(w, "No API endpoint received")
				return
			}
			if err := ValidateEndpointURL(endpoint); err != nil {
				errCh <- fmt.Errorf("invalid API endpoint: %w", err)
				renderErrorPage(w, "Invalid API endpoint")
				return
			}

			exchangeResult, err := exchangeCodeForToken(ctx, endpoint, code, codeVerifier)
			if err != nil {
				errCh <- fmt.Errorf("token exchange failed: %w", err)
				renderErrorPage(w, "Token exchange failed: "+err.Error())
				return
			}

			result := &Result{
				Token:            exchangeResult.Data.Token,
				Email:            exchangeResult.Data.Email,
				DeviceName:       r.URL.Query().Get("device"),
				APIEndpoint:      exchangeResult.Data.APIEndpoint,
				ExpiresAt:        exchangeResult.Data.ExpiresAt,
				RefreshToken:     exchangeResult.Data.RefreshToken,
				RefreshExpiresAt: exchangeResult.Data.RefreshExpiresAt,
			}

			resultCh <- result
			renderSuccessPage(w)
		})
	})

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", bindAddress, port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	return server
}

var allowedDomainSuffixes = []string{
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

	if (!strings.Contains(hostname, ".") && !strings.Contains(hostname, ":")) || hostname == "127.0.0.1" || hostname == "::1" {
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

	exchangeURL := endpoint + "/api/cli/v1/auth/exchange"

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read exchange response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchange returned status %d: %s", resp.StatusCode, string(respBody))
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

	return &result, nil
}

func findAvailablePort(bindAddress string) (int, error) {
	for port := 54321; port < 54400; port++ {
		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bindAddress, port))
		if err == nil {
			_ = listener.Close()
			return port, nil
		}
	}
	return 0, errors.New("no available port in range 54321-54399")
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

func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

func renderSuccessPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if err := successTemplate.Execute(&buf, nil); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

func renderErrorPage(w http.ResponseWriter, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	data := struct{ Error string }{Error: html.EscapeString(errMsg)}
	var buf bytes.Buffer
	if err := errorTemplate.Execute(&buf, data); err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}
