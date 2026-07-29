package grafana_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	servergrafana "github.com/grafana/gcx/internal/server/grafana"
)

func TestAuthenticateAndProxyHandlerUsesOnlySelectedAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		grafana           config.GrafanaConfig
		wantAuthorization string
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
			grafanaCfg.StackID = 12345
			ctx := &config.Context{Name: "selected", Grafana: &grafanaCfg}
			handler := servergrafana.AuthenticateAndProxyHandler(ctx)

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
			if gotRequestURI != "/api/example?panel=1" {
				t.Errorf("request URI = %q, want query and path preserved", gotRequestURI)
			}
		})
	}
}

func TestAuthenticateAndProxyHandlerRejectsUnsupportedAuthBeforeNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		grafana     config.GrafanaConfig
		wantMessage string
	}{
		{
			name: "partial token",
			grafana: config.GrafanaConfig{
				AuthMethod: "token",
			},
			wantMessage: "requires a non-empty Grafana service-account token",
		},
		{
			name: "OAuth without persistence wiring",
			grafana: config.GrafanaConfig{
				AuthMethod:        "oauth",
				OAuthToken:        "oauth-access",
				OAuthRefreshToken: "oauth-refresh",
			},
			wantMessage: "OAuth authentication is not supported by `gcx dev serve`",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requests := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(upstream.Close)

			grafanaCfg := tc.grafana
			grafanaCfg.Server = upstream.URL
			grafanaCfg.ProxyEndpoint = upstream.URL
			grafanaCfg.StackID = 12345
			handler := servergrafana.AuthenticateAndProxyHandler(&config.Context{Name: "selected", Grafana: &grafanaCfg})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/example", nil))

			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			if requests != 0 {
				t.Fatalf("upstream requests = %d, want 0", requests)
			}
			if !strings.Contains(response.Body.String(), tc.wantMessage) {
				t.Errorf("body %q does not contain %q", response.Body.String(), tc.wantMessage)
			}
		})
	}
}
