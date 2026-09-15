package grafana_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestValidateDevProxyAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		context     *config.Context
		wantMessage string
	}{
		{
			name:        "nil context",
			context:     nil,
			wantMessage: "no Grafana URL configured",
		},
		{
			name:        "no grafana config",
			context:     &config.Context{Name: "selected"},
			wantMessage: "no Grafana URL configured",
		},
		{
			name:        "empty server",
			context:     &config.Context{Name: "selected", Grafana: &config.GrafanaConfig{AuthMethod: "token", APIToken: "token"}},
			wantMessage: "no Grafana URL configured",
		},
		{
			name: "partial token",
			context: &config.Context{Name: "selected", Grafana: &config.GrafanaConfig{
				Server:     "https://stack.example.invalid",
				AuthMethod: "token",
			}},
			wantMessage: `auth-method "token" requires a non-empty Grafana service-account token`,
		},
		{
			name: "token accepted",
			context: &config.Context{Name: "selected", Grafana: &config.GrafanaConfig{
				Server:     "https://stack.example.invalid",
				AuthMethod: "token",
				APIToken:   "selected-token",
			}},
		},
		{
			name: "basic accepted",
			context: &config.Context{Name: "selected", Grafana: &config.GrafanaConfig{
				Server:     "https://stack.example.invalid",
				AuthMethod: "basic",
				User:       "selected-user",
				Password:   "selected-password",
			}},
		},
		{
			name: "OAuth accepted",
			context: &config.Context{Name: "selected", Grafana: &config.GrafanaConfig{
				Server:            "https://stack.example.invalid",
				ProxyEndpoint:     "https://proxy.example.invalid",
				AuthMethod:        "oauth",
				OAuthToken:        "oauth-access",
				OAuthRefreshToken: "oauth-refresh",
			}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := servergrafana.ValidateDevProxyAuth(tc.context)
			if tc.wantMessage == "" {
				if err != nil {
					t.Fatalf("ValidateDevProxyAuth() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateDevProxyAuth() error = nil, want %q", tc.wantMessage)
			}
			if !strings.Contains(err.Error(), tc.wantMessage) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantMessage)
			}
		})
	}
}
