// Tests inspect the in-memory cache and inject a mock issuer without a production override.
//
//nolint:testpackage
package auth

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubActionsExchangeCacheRefreshAndResponseBinding(t *testing.T) {
	for _, name := range []string{"valid", "opaque token", "long lifetime", "empty token", "short lifetime", "tenant", "endpoint", "scope", "expiry", "destination", "secret error"} {
		t.Run(name, func(t *testing.T) {
			var oidcCalls, exchangeCalls atomic.Int32
			var endpoint string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/oidc" {
					oidcCalls.Add(1)
					assert.Equal(t, "Bearer request-secret", r.Header.Get("Authorization"))
					assert.Equal(t, "gcx", r.URL.Query().Get("audience"))
					_, _ = io.WriteString(w, `{"value":"signed-oidc-secret"}`)
					return
				}
				exchangeCalls.Add(1)
				assert.Equal(t, "/api/cli/v1/auth/github-actions", r.URL.Path)
				assert.Equal(t, "POST", r.Method)
				var body map[string]any
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "signed-oidc-secret", body["oidc_token"])
				assert.Empty(t, r.Header.Get("Authorization"))
				result := GitHubActionsResult{Token: "gat_secret", Tenant: "1", ExpiresAt: time.Now().Add(15 * time.Minute), APIEndpoint: endpoint, GrafanaURL: "https://stack.grafana.net", Scopes: []string{"assistant:chat"}}
				switch name {
				case "tenant":
					result.Tenant = "2"
				case "endpoint":
					result.APIEndpoint = "https://other.invalid"
				case "scope":
					result.Scopes = []string{"grafana-api:write"}
				case "opaque token":
					result.Token = "opaque-credential"
				case "long lifetime":
					result.ExpiresAt = time.Now().Add(time.Hour)
				case "empty token":
					result.Token = ""
				case "short lifetime":
					result.ExpiresAt = time.Now().Add(30 * time.Second)
				case "expiry":
					result.ExpiresAt = time.Now().Add(-time.Minute)
				case "destination":
					result.GrafanaURL = "https://other.grafana.net"
				case "secret error":
					http.Error(w, "signed-oidc-secret gat_secret", http.StatusUnauthorized)
					return
				}
				assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": result}))
			}))
			defer srv.Close()
			endpoint = srv.URL
			a := &GitHubActions{options: GitHubActionsOptions{Endpoint: endpoint, TenantID: "1", Scopes: []string{"assistant:chat"}, GrafanaURL: "https://stack.grafana.net"}, requestURL: endpoint + "/oidc", requestToken: "request-secret", client: srv.Client()}
			token, err := a.FreshToken(context.Background())
			if name != "valid" && name != "opaque token" && name != "long lifetime" {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret")
				return
			}
			require.NoError(t, err)
			if name == "opaque token" {
				require.Equal(t, "opaque-credential", token)
			} else {
				require.Equal(t, "gat_secret", token)
			}
			var wg sync.WaitGroup
			for range 10 {
				wg.Go(func() { _, err := a.FreshToken(context.Background()); require.NoError(t, err) })
			}
			wg.Wait()
			require.EqualValues(t, 1, oidcCalls.Load())
			require.EqualValues(t, 1, exchangeCalls.Load())
			a.result.ExpiresAt = time.Now().Add(30 * time.Second)
			_, err = a.FreshToken(context.Background())
			require.NoError(t, err)
			require.EqualValues(t, 2, oidcCalls.Load())
		})
	}
}

type actionsRoundTripper func(*http.Request) (*http.Response, error)

func (f actionsRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGitHubActionsTransportRestrictsCredentials(t *testing.T) {
	a := &GitHubActions{options: GitHubActionsOptions{Endpoint: "https://backend.example"}, result: GitHubActionsResult{Token: "gat_secret", ExpiresAt: time.Now().Add(10 * time.Minute)}}
	calls := 0
	transport := &GitHubActionsTransport{Auth: a, Base: actionsRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "Bearer gat_secret", r.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, url := range []string{"https://backend.example/api/cli/v1/proxy/api/user", "https://attacker.example/api/cli/v1/proxy", "http://backend.example/api/cli/v1/proxy", "https://backend.example/api/v1/other"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader("mutation"))
		require.NoError(t, err)
		response, err := transport.RoundTrip(req)
		if strings.Contains(url, "proxy/api/user") {
			require.NoError(t, err)
			response.Body.Close()
		} else {
			require.Error(t, err)
		}
		require.Empty(t, req.Header.Get("Authorization"), "original request must not retain credential")
	}
	require.Equal(t, 1, calls, "this auth transport does not replay requests; shared retries are separate")
}
func TestGitHubActionsEnvironmentAndConfigValidation(t *testing.T) {
	good := GitHubActionsOptions{Endpoint: "https://assistant.example", TenantID: "1", Scopes: []string{"assistant:chat"}}
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "secret")
	for _, raw := range []string{
		"",
		"http://pipelines.actions.githubusercontent.com/oidc",
		"https://attacker.example/oidc",
		"https://actions.githubusercontent.com.attacker.example/oidc",
		"https://user:pass@pipelines.actions.githubusercontent.com/oidc", // trufflehog:ignore -- dummy credentials test userinfo rejection.
	} {
		t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", raw)
		_, err := NewGitHubActions(good)
		require.Error(t, err)
	}
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://pipelines.actions.githubusercontent.com/oidc?api-version=1")
	_, err := NewGitHubActions(good)
	require.NoError(t, err)
	for _, raw := range []string{"http://remote.example", "https://user:pass@backend.example", "https://backend.example?token=secret", "https://backend.example#fragment"} {
		options := good
		options.Endpoint = raw
		require.Error(t, options.Validate())
	}
	options := good
	options.Scopes = []string{"assistant:chat", "assistant:chat"}
	require.Error(t, options.Validate())
}

func TestGitHubActionsTransportErrorsAreUsefulAndSanitized(t *testing.T) {
	tests := []struct {
		name     string
		cause    error
		want     string
		sentinel error
	}{
		{name: "cancelled", cause: context.Canceled, want: "context canceled", sentinel: context.Canceled},
		{name: "deadline", cause: context.DeadlineExceeded, want: "context deadline exceeded", sentinel: context.DeadlineExceeded},
		{name: "DNS", cause: &net.DNSError{Name: "secret.example", Err: "secret"}, want: "DNS lookup failed"},
		{name: "TLS", cause: &tls.CertificateVerificationError{Err: errors.New("secret")}, want: "TLS certificate verification failed"},
		{name: "timeout", cause: &net.OpError{Op: "secret", Err: os.ErrDeadlineExceeded}, want: "network timeout"},
		{name: "connection", cause: errors.New("secret"), want: "connection failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &GitHubActions{client: &http.Client{Transport: actionsRoundTripper(func(*http.Request) (*http.Response, error) {
				return nil, &url.Error{Op: "GET", URL: "https://secret.example/?token=secret", Err: tt.cause}
			})}}
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://secret.example", nil)
			require.NoError(t, err)
			err = client.do(req, &struct{}{})
			require.ErrorContains(t, err, tt.want)
			require.NotContains(t, err.Error(), "secret")
			if tt.sentinel != nil {
				require.ErrorIs(t, err, tt.sentinel)
			}
		})
	}
}

func TestGitHubActionsSharedClientRejectsRedirects(t *testing.T) {
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "secret")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://pipelines.actions.githubusercontent.com/oidc")
	client, err := NewGitHubActions(GitHubActionsOptions{Endpoint: "https://assistant.example", TenantID: "1", Scopes: []string{"assistant:chat"}})
	require.NoError(t, err)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Contains(t, r.UserAgent(), "gcx/")
		http.Redirect(w, r, "/must-not-follow", http.StatusFound)
	}))
	defer server.Close()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	err = client.do(req, &struct{}{})
	require.ErrorContains(t, err, "HTTP 302")
	require.Equal(t, 1, calls)
}
