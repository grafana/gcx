package assistant_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/assistant"
)

func TestNewAssistantStreamingHTTPClient(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input int
		want  time.Duration
	}{
		{"positive value", 420, 420 * time.Second},
		{"non-positive defaults to 300s", 0, 300 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := assistant.NewAssistantStreamingHTTPClient(context.Background(), tc.input)
			if c.Timeout != tc.want {
				t.Fatalf("Timeout: got %v, want %v", c.Timeout, tc.want)
			}
		})
	}
}

func TestNewAssistantStreamingHTTPClientForRESTConfigMaterializesTLSRefreshTransport(t *testing.T) {
	var refreshCalls, apiCalls int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/v1/auth/refresh":
			refreshCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_new",
					"expires_at":         time.Now().Add(time.Hour).Format(time.RFC3339),
					"refresh_token":      "gar_new",
					"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			})
		default:
			apiCalls++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	caData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), config.Context{
		Grafana: &config.GrafanaConfig{
			Server:                server.URL,
			ProxyEndpoint:         server.URL,
			OAuthToken:            "gat_old",
			OAuthRefreshToken:     "gar_old",
			OAuthTokenExpiresAt:   time.Now().Add(-time.Hour).Format(time.RFC3339),
			OAuthRefreshExpiresAt: time.Now().Add(24 * time.Hour).Format(time.RFC3339),
			StackID:               1,
			TLS:                   &config.TLS{CAData: caData},
		},
	})
	if err != nil {
		t.Fatalf("NewNamespacedRESTConfig() error = %v", err)
	}
	restCfg.SetOnRefresh(func(_, _, _, _, _ string) error { return nil })

	client, err := assistant.NewAssistantStreamingHTTPClientForRESTConfig(&restCfg.Config, 17)
	if err != nil {
		t.Fatalf("NewAssistantStreamingHTTPClientForRESTConfig() error = %v", err)
	}
	if client.Timeout != 17*time.Second {
		t.Fatalf("Timeout = %v, want %v", client.Timeout, 17*time.Second)
	}

	token, err := restCfg.FreshOAuthToken(t.Context())
	if err != nil {
		t.Fatalf("FreshOAuthToken() error = %v", err)
	}
	if token != "gat_new" {
		t.Fatalf("FreshOAuthToken() = %q, want %q", token, "gat_new")
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", refreshCalls)
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/api/cli/v1/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("TLS-aware A2A request error = %v", err)
	}
	_ = resp.Body.Close()
	if apiCalls != 1 {
		t.Fatalf("API calls = %d, want 1", apiCalls)
	}
}
