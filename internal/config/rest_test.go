package config_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authlib "github.com/grafana/authlib/types"
	"github.com/grafana/gcx/internal/config"
)

func TestNewNamespacedRESTConfig_UsesBootdataStack(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-98765",
			},
		})
	}))
	defer bootdataServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL + "/grafana",
			StackID: 12345,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	if got, want := restCfg.Namespace, authlib.CloudNamespaceFormatter(98765); got != want {
		t.Fatalf("expected namespace %s, got %s", want, got)
	}

	if ctx.Grafana.StackID != 12345 {
		t.Fatalf("expected original stack ID to remain unchanged, got %d", ctx.Grafana.StackID)
	}
}

func TestNewNamespacedRESTConfig_FallsBackOnBootdataError(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bootdataServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL,
			StackID: 555,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	if got, want := restCfg.Namespace, authlib.CloudNamespaceFormatter(555); got != want {
		t.Fatalf("expected namespace %s, got %s", want, got)
	}
}

func TestNewNamespacedRESTConfig_FallsBackWhenBootdataNotStack(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "grafana",
			},
		})
	}))
	defer bootdataServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL,
			StackID: 42,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	if got, want := restCfg.Namespace, authlib.CloudNamespaceFormatter(42); got != want {
		t.Fatalf("expected namespace %s, got %s", want, got)
	}
}

func TestNewNamespacedRESTConfig_TrimsTrailingSlash(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bootdataServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL + "/",
			StackID: 1,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	if restCfg.Host != bootdataServer.URL {
		t.Fatalf("expected trailing slash to be trimmed: got %q, want %q", restCfg.Host, bootdataServer.URL)
	}
}

func TestNewNamespacedRESTConfig_OAuthProxyTrimsTrailingSlash(t *testing.T) {
	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:        "https://mystack.grafana.net",
			ProxyEndpoint: "https://mystack.grafana.net/a/grafana-assistant-app/",
			OAuthToken:    "gat_test-token",
			StackID:       123,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	expectedHost := "https://mystack.grafana.net/a/grafana-assistant-app/api/cli/v1/proxy"
	if restCfg.Host != expectedHost {
		t.Fatalf("expected Host %q, got %q", expectedHost, restCfg.Host)
	}
}

func TestNewNamespacedRESTConfig_OAuthProxySetsHost(t *testing.T) {
	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:        "https://mystack.grafana.net",
			ProxyEndpoint: "https://mystack.grafana.net/a/grafana-assistant-app",
			OAuthToken:    "gat_test-token",
			StackID:       123,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	expectedHost := "https://mystack.grafana.net/a/grafana-assistant-app/api/cli/v1/proxy"
	if restCfg.Host != expectedHost {
		t.Fatalf("expected Host %q, got %q", expectedHost, restCfg.Host)
	}
}

func TestNamespacedRESTConfig_SetOnRefresh(t *testing.T) {
	refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/cli/v1/auth/refresh" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_new",
					"expires_at":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
					"refresh_token":      "gar_new",
					"refresh_expires_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer refreshServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:              refreshServer.URL,
			ProxyEndpoint:       refreshServer.URL,
			OAuthToken:          "gat_expiring",
			OAuthRefreshToken:   "gar_old",
			OAuthTokenExpiresAt: time.Now().Add(1 * time.Minute).Format(time.RFC3339),
			StackID:             123,
		},
	}

	restCfg := config.NewNamespacedRESTConfig(t.Context(), ctx)

	var callbackCalled bool
	restCfg.SetOnRefresh(func(token, refreshToken, expiresAt, refreshExpiresAt string) error {
		callbackCalled = true
		return nil
	})

	// Make a request to trigger the refresh.
	if restCfg.WrapTransport == nil {
		t.Fatal("expected WrapTransport to be set for OAuth proxy mode")
	}
	rt := restCfg.WrapTransport(http.DefaultTransport)

	client := &http.Client{Transport: rt}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, refreshServer.URL+"/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if !callbackCalled {
		t.Fatal("expected OnRefresh callback to be called after token refresh")
	}
}
