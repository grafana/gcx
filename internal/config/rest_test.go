package config_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	authlib "github.com/grafana/authlib/types"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/retry"
)

func TestNewNamespacedRESTConfig_PropagatesTLSConfig(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bootdataServer.Close()

	certData := []byte("cert-pem-data")
	keyData := []byte("key-pem-data")
	caData := []byte("ca-pem-data")

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL,
			StackID: 1,
			TLS: &config.TLS{
				CertData:   certData,
				KeyData:    keyData,
				CAData:     caData,
				Insecure:   true,
				ServerName: "custom-sni.example.com",
				NextProtos: []string{"http/1.1"},
			},
		},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tls := restCfg.TLSClientConfig
	if string(tls.CertData) != string(certData) {
		t.Fatalf("CertData not propagated: got %q", tls.CertData)
	}
	if string(tls.KeyData) != string(keyData) {
		t.Fatalf("KeyData not propagated: got %q", tls.KeyData)
	}
	if string(tls.CAData) != string(caData) {
		t.Fatalf("CAData not propagated: got %q", tls.CAData)
	}
	if !tls.Insecure {
		t.Fatal("Insecure not propagated")
	}
	if tls.ServerName != "custom-sni.example.com" {
		t.Fatalf("ServerName not propagated: got %q", tls.ServerName)
	}
	if len(tls.NextProtos) != 1 || tls.NextProtos[0] != "http/1.1" {
		t.Fatalf("NextProtos not propagated: got %v", tls.NextProtos)
	}
}

func TestNewNamespacedRESTConfig_NilTLSLeavesDefaults(t *testing.T) {
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bootdataServer.Close()

	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:  bootdataServer.URL,
			StackID: 1,
		},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tls := restCfg.TLSClientConfig
	if len(tls.CertData) != 0 || len(tls.KeyData) != 0 || len(tls.CAData) != 0 {
		t.Fatal("expected empty TLS data when no TLS config is set")
	}
	if tls.Insecure {
		t.Fatal("expected Insecure to be false by default")
	}
}

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

	restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)

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

	restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)

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

	restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)

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

	restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)

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

	restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)

	expectedHost := "https://mystack.grafana.net/a/grafana-assistant-app/api/cli/v1/proxy"
	if restCfg.Host != expectedHost {
		t.Fatalf("expected Host %q, got %q", expectedHost, restCfg.Host)
	}
}

func TestNamespacedRESTConfig_IsOAuthProxy(t *testing.T) {
	t.Run("true when OAuth configured", func(t *testing.T) {
		ctx := config.Context{
			Grafana: &config.GrafanaConfig{
				Server:        "https://mystack.grafana.net",
				ProxyEndpoint: "https://mystack.grafana.net/a/grafana-assistant-app",
				OAuthToken:    "gat_test-token",
				StackID:       123,
			},
		}
		restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)
		if !restCfg.IsOAuthProxy() {
			t.Fatal("expected IsOAuthProxy() to return true for OAuth config")
		}
	})

	t.Run("false when token auth", func(t *testing.T) {
		ctx := config.Context{
			Grafana: &config.GrafanaConfig{
				Server:   "https://mystack.grafana.net",
				APIToken: "glsa_test-token",
				StackID:  123,
			},
		}
		restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)
		if restCfg.IsOAuthProxy() {
			t.Fatal("expected IsOAuthProxy() to return false for token auth config")
		}
	})
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

	restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)

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

	restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)

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
	retryRT, ok := rt.(*retry.Transport)
	if !ok {
		t.Fatalf("expected outermost transport to be *retry.Transport, got %T", rt)
	}
	if _, ok := retryRT.Base.(*httputils.LoggingRoundTripper); !ok {
		t.Fatalf("expected retry.Transport.Base to be *httputils.LoggingRoundTripper, got %T", retryRT.Base)
	}

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
