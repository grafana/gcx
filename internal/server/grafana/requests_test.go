package grafana_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	servergrafana "github.com/grafana/gcx/internal/server/grafana"
)

func TestAuthenticateAndProxyHandlerUsesOnlySelectedAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		grafana           config.GrafanaConfig
		wantAuthorization string
		wantRequestURI    string
	}{
		{
			name: "token ignores stale Basic credentials",
			grafana: config.GrafanaConfig{
				AuthMethod: "token",
				APIToken:   "selected-token",
				User:       "stale-user",
				Password:   "stale-password",
			},
			wantAuthorization: "Bearer selected-token",
			wantRequestURI:    "/api/example?panel=1",
		},
		{
			name: "Basic ignores stale token",
			grafana: config.GrafanaConfig{
				AuthMethod: "basic",
				APIToken:   "stale-token",
				User:       "selected-user",
				Password:   "selected-password",
			},
			wantAuthorization: "Basic c2VsZWN0ZWQtdXNlcjpzZWxlY3RlZC1wYXNzd29yZA==",
			wantRequestURI:    "/api/example?panel=1",
		},
		{
			name: "OAuth proxies through the proxy endpoint",
			grafana: config.GrafanaConfig{
				AuthMethod:          "oauth",
				APIToken:            "stale-token",
				OAuthToken:          "oauth-access",
				OAuthTokenExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
			},
			wantAuthorization: "Bearer oauth-access",
			// OAuth rewrites Host to the proxy endpoint, and the proxy path
			// prefix must survive into the target URL.
			wantRequestURI: "/api/cli/v1/proxy/api/example?panel=1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotAuthorization, gotRequestURI string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotAuthorization = r.Header.Get("Authorization")
				gotRequestURI = r.URL.RequestURI()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("proxied"))
			}))
			t.Cleanup(upstream.Close)

			grafanaCfg := tc.grafana
			grafanaCfg.Server = upstream.URL
			grafanaCfg.ProxyEndpoint = upstream.URL
			grafanaCfg.StackID = 12345
			cfgCtx := &config.Context{Name: "selected", Grafana: &grafanaCfg}
			restCfg, err := cfgCtx.ToRESTConfig(context.Background())
			if err != nil {
				t.Fatalf("ToRESTConfig() error = %v", err)
			}
			handler := servergrafana.AuthenticateAndProxyHandler(restCfg)

			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/example?panel=1", nil)
			req.Header.Set("Authorization", "Bearer browser-supplied")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
			}
			if gotAuthorization != tc.wantAuthorization {
				t.Errorf("Authorization = %q, want %q", gotAuthorization, tc.wantAuthorization)
			}
			if gotRequestURI != tc.wantRequestURI {
				t.Errorf("request URI = %q, want %q", gotRequestURI, tc.wantRequestURI)
			}
		})
	}
}

// Concurrent proxy requests must share one HTTP client. Building it per request
// re-runs the REST config's WrapTransport, which mutates the shared OAuth
// RefreshTransport and races under -race.
func TestAuthenticateAndProxyHandlerServesConcurrentOAuthRequests(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied"))
	}))
	t.Cleanup(upstream.Close)

	cfgCtx := &config.Context{Name: "selected", Grafana: &config.GrafanaConfig{
		AuthMethod:          "oauth",
		OAuthToken:          "oauth-access",
		OAuthTokenExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339),
		Server:              upstream.URL,
		ProxyEndpoint:       upstream.URL,
		StackID:             12345,
	}}
	restCfg, err := cfgCtx.ToRESTConfig(context.Background())
	if err != nil {
		t.Fatalf("ToRESTConfig() error = %v", err)
	}
	handler := servergrafana.AuthenticateAndProxyHandler(restCfg)

	var wg sync.WaitGroup
	codes := make([]int, 16)
	for i := range codes {
		wg.Go(func() {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/example", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			codes[i] = response.Code
		})
	}
	wg.Wait()

	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d: status = %d, want %d", i, code, http.StatusOK)
		}
	}
}
