package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

const GitHubActionsAudience = "gcx"

// GitHubActionsOptions contains no credentials. The endpoint is explicitly trusted by login.
type GitHubActionsOptions struct {
	Endpoint   string   `json:"endpoint" yaml:"endpoint"`
	TenantID   string   `json:"tenant-id" yaml:"tenant-id"`
	Scopes     []string `json:"scopes" yaml:"scopes"`
	GrafanaURL string   `json:"-" yaml:"-"`
}

// Validate validates complete configuration without requesting credentials.
func (o GitHubActionsOptions) Validate() error {
	if _, err := trustedActionsEndpoint(o.Endpoint); err != nil {
		return err
	}
	if o.TenantID == "" || len(o.Scopes) == 0 {
		return errors.New("GitHub Actions requires tenant-id and explicit scopes")
	}
	valid := []string{"assistant:a2a", "assistant:chat", "grafana-api:read", "grafana-api:write", "grafana-api:delete"}
	seen := map[string]bool{}
	for _, scope := range o.Scopes {
		if !slices.Contains(valid, scope) || seen[scope] {
			return fmt.Errorf("invalid or duplicate GitHub Actions scope %q", scope)
		}
		seen[scope] = true
	}
	return nil
}

// GitHubActions obtains credentials on demand and retains them only in this process.
type GitHubActions struct {
	options      GitHubActionsOptions
	client       *http.Client
	requestURL   string
	requestToken string
	mu           sync.Mutex
	result       GitHubActionsResult
}

type GitHubActionsResult struct {
	Token       string    `json:"token"`
	Tenant      string    `json:"tenant"`
	ExpiresAt   time.Time `json:"expires_at"`
	APIEndpoint string    `json:"api_endpoint"`
	GrafanaURL  string    `json:"grafana_url"`
	Scopes      []string  `json:"scopes"`
}

func NewGitHubActions(options GitHubActionsOptions) (*GitHubActions, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	raw, token := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL"), os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Hostname() != "token.actions.githubusercontent.com" && !strings.HasSuffix(u.Hostname(), ".actions.githubusercontent.com")) || token == "" {
		return nil, errors.New("GitHub Actions OIDC is unavailable; run in a GitHub Actions job with id-token: write")
	}
	return &GitHubActions{options: options, requestURL: raw, requestToken: token, client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (a *GitHubActions) FreshToken(ctx context.Context) (string, error) {
	result, err := a.Exchange(ctx)
	if err != nil {
		return "", err
	}
	return result.Token, nil
}

// Exchange caches a credential until one minute before expiry. It does not replay API requests.
func (a *GitHubActions) Exchange(ctx context.Context) (GitHubActionsResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.result.Token != "" && time.Until(a.result.ExpiresAt) > time.Minute {
		return a.result, nil
	}
	var empty GitHubActionsResult
	u, err := url.Parse(a.requestURL)
	if err != nil {
		return empty, errors.New("invalid GitHub OIDC request URL")
	}
	q := u.Query()
	q.Set("audience", GitHubActionsAudience)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return empty, err
	}
	req.Header.Set("Authorization", "Bearer "+a.requestToken)
	var oidc struct {
		Value string `json:"value"`
	}
	if err := a.do(req, &oidc); err != nil {
		return empty, fmt.Errorf("request GitHub OIDC token: %w", err)
	}
	if oidc.Value == "" {
		return empty, errors.New("GitHub returned no OIDC token")
	}
	body, err := json.Marshal(struct {
		Token  string   `json:"oidc_token"`
		Tenant string   `json:"tenant_id"`
		Scopes []string `json:"scopes"`
	}{oidc.Value, a.options.TenantID, a.options.Scopes})
	if err != nil {
		return empty, err
	}
	endpoint := strings.TrimRight(a.options.Endpoint, "/")
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/cli/v1/auth/github-actions", bytes.NewReader(body))
	if err != nil {
		return empty, err
	}
	req.Header.Set("Content-Type", "application/json")
	var response struct {
		Data GitHubActionsResult `json:"data"`
	}
	if err := a.do(req, &response); err != nil {
		return empty, fmt.Errorf("exchange GitHub identity: %w; check the workflow grant and linked Grafana account", err)
	}
	r := response.Data
	if !strings.HasPrefix(r.Token, "gat_") || r.Tenant != a.options.TenantID || strings.TrimRight(r.APIEndpoint, "/") != endpoint || time.Until(r.ExpiresAt) <= time.Minute || time.Until(r.ExpiresAt) > 16*time.Minute {
		return empty, errors.New("invalid GitHub Actions credential response")
	}
	if _, err := trustedActionsEndpoint(r.GrafanaURL); err != nil {
		return empty, errors.New("invalid Grafana URL in credential response")
	}
	if a.options.GrafanaURL != "" && strings.TrimRight(r.GrafanaURL, "/") != strings.TrimRight(a.options.GrafanaURL, "/") {
		return empty, errors.New("GitHub Actions credential changed Grafana destination; run login again")
	}
	requested, granted := slices.Clone(a.options.Scopes), slices.Clone(r.Scopes)
	slices.Sort(requested)
	slices.Sort(granted)
	if !slices.Equal(requested, granted) {
		return empty, errors.New("GitHub Actions exchange returned different scopes")
	}
	a.result = r
	return r, nil
}

func (a *GitHubActions) do(req *http.Request, out any) error {
	res, err := a.client.Do(req)
	if err != nil {
		return errors.New("authentication service unavailable")
	}
	defer res.Body.Close()
	// Never include remote bodies or URLs: either can contain authentication material.
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 128*1024)).Decode(out); err != nil {
		return errors.New("invalid authentication response")
	}
	return nil
}

func trustedActionsEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid authentication endpoint")
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && (!loopback || u.Scheme != "http")) {
		return nil, errors.New("endpoint must be an explicit HTTPS URL, or loopback HTTP, without credentials, query or fragment")
	}
	return u, nil
}

// GitHubActionsTransport limits the credential to the explicitly selected CLI backend.
type GitHubActionsTransport struct {
	Base http.RoundTripper
	Auth *GitHubActions
}

func (t *GitHubActionsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	endpoint, err := trustedActionsEndpoint(t.Auth.options.Endpoint)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != endpoint.Scheme || req.URL.Host != endpoint.Host || !strings.HasPrefix(req.URL.Path, strings.TrimRight(endpoint.Path, "/")+"/api/cli/v1/") {
		return nil, errors.New("refusing GitHub Actions credential outside its backend")
	}
	token, err := t.Auth.FreshToken(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+token)
	return t.Base.RoundTrip(clone)
}
