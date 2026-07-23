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

func TestNewNamespacedRESTConfig_ConfiguredStackIDSkipsBootdata(t *testing.T) {
	var hits int
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
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

	// A configured StackID is authoritative: no /bootdata round-trip is made and
	// the namespace reflects the configured stack, not the server's response.
	if hits != 0 {
		t.Fatalf("expected no bootdata requests, got %d", hits)
	}
	if got, want := restCfg.Namespace, authlib.CloudNamespaceFormatter(12345); got != want {
		t.Fatalf("expected namespace %s, got %s", want, got)
	}
}

func TestNamespacedRESTConfig_StackID(t *testing.T) {
	cases := []struct {
		namespace string
		want      int64
	}{
		{"stacks-12345", 12345},
		{"org-5", 0},   // on-prem org namespace: not a stack
		{"default", 0}, // org-1
		{"", 0},        // unresolved
		{"garbage", 0},
	}
	for _, tc := range cases {
		rc := config.NamespacedRESTConfig{Namespace: tc.namespace}
		if got := rc.StackID(); got != tc.want {
			t.Errorf("StackID() for namespace %q = %d, want %d", tc.namespace, got, tc.want)
		}
	}
}

func TestNewNamespacedRESTConfig_DiscoversAndCachesStackID(t *testing.T) {
	var hits int
	bootdataServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-77777",
			},
		})
	}))
	defer bootdataServer.Close()

	// No StackID/OrgID configured: the stack ID is discovered via /bootdata.
	ctx := config.Context{
		Grafana: &config.GrafanaConfig{Server: bootdataServer.URL},
	}

	for i := range 2 {
		restCfg, _ := config.NewNamespacedRESTConfig(t.Context(), ctx)
		if got, want := restCfg.Namespace, authlib.CloudNamespaceFormatter(77777); got != want {
			t.Fatalf("call %d: expected namespace %s, got %s", i, want, got)
		}
	}

	// The second build reuses the cached discovery instead of hitting the server.
	if hits != 1 {
		t.Fatalf("expected exactly 1 bootdata request (cached thereafter), got %d", hits)
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

func TestNewNamespacedRESTConfig_ExecProvider(t *testing.T) {
	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:     "https://grafana.example.net",
			OrgID:      1,
			AuthMethod: "exec",
			Exec: &config.ExecConfig{
				Command:         "gcx-token-helper",
				Args:            []string{"--audience", "grafana"},
				Env:             []config.ExecEnvVar{{Name: "FOO", Value: "bar"}},
				InstallHint:     "go install example.com/gcx-token-helper@latest",
				InteractiveMode: "Never",
			},
		},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := restCfg.ExecProvider
	if ep == nil {
		t.Fatal("expected ExecProvider to be set for exec auth")
	}
	if ep.APIVersion != "client.authentication.k8s.io/v1" {
		t.Fatalf("expected stable v1 APIVersion, got %q", ep.APIVersion)
	}
	if ep.Command != "gcx-token-helper" {
		t.Fatalf("expected command gcx-token-helper, got %q", ep.Command)
	}
	if len(ep.Args) != 2 || ep.Args[0] != "--audience" || ep.Args[1] != "grafana" {
		t.Fatalf("unexpected args: %v", ep.Args)
	}
	if len(ep.Env) != 1 || ep.Env[0].Name != "FOO" || ep.Env[0].Value != "bar" {
		t.Fatalf("unexpected env: %v", ep.Env)
	}
	if ep.InstallHint != "go install example.com/gcx-token-helper@latest" {
		t.Fatalf("unexpected install hint: %q", ep.InstallHint)
	}
	if string(ep.InteractiveMode) != "Never" {
		t.Fatalf("expected InteractiveMode Never, got %q", ep.InteractiveMode)
	}
	// No static bearer token should be set; the exec provider supplies it.
	if restCfg.BearerToken != "" {
		t.Fatalf("expected no static BearerToken, got %q", restCfg.BearerToken)
	}
}

func TestNewNamespacedRESTConfig_ExecProviderDefaultInteractiveMode(t *testing.T) {
	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:     "https://grafana.example.net",
			OrgID:      1,
			AuthMethod: "exec",
			Exec:       &config.ExecConfig{Command: "gcx-token-helper"},
		},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restCfg.ExecProvider == nil {
		t.Fatal("expected ExecProvider to be set")
	}
	if got := string(restCfg.ExecProvider.InteractiveMode); got != "IfAvailable" {
		t.Fatalf("expected default InteractiveMode IfAvailable, got %q", got)
	}
}

// TestNewNamespacedRESTConfig_ExecWinsOverStaleOAuthFields guards auth-method
// precedence: an explicit `auth-method: exec` must route through the exec
// provider even when stale OAuth proxy fields linger in the config (e.g. left
// over from a prior `gcx login`, or merged in from a lower config layer). The
// auth switch keys each arm on the effective method, so the OAuth arm must not
// shadow exec on raw field presence alone.
func TestNewNamespacedRESTConfig_ExecWinsOverStaleOAuthFields(t *testing.T) {
	ctx := config.Context{
		Grafana: &config.GrafanaConfig{
			Server:     "https://grafana.example.net",
			OrgID:      1,
			AuthMethod: "exec",
			Exec:       &config.ExecConfig{Command: "gcx-token-helper"},
			// Stale OAuth fields that would otherwise match the OAuth arm.
			ProxyEndpoint: "https://leftover.grafana.net/a/grafana-assistant-app",
			OAuthToken:    "gat_stale-token",
		},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if restCfg.ExecProvider == nil {
		t.Fatal("expected ExecProvider to be set; exec auth was shadowed by stale OAuth fields")
	}
	if restCfg.IsOAuthProxy() {
		t.Fatal("expected exec auth, but config was routed through the OAuth proxy")
	}
	// Host must remain the plain server, not be rewritten to the OAuth proxy path.
	if restCfg.Host != "https://grafana.example.net" {
		t.Fatalf("expected Host to stay the server URL, got %q", restCfg.Host)
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
	callerRT, ok := rt.(*httputils.CallerIDTransport)
	if !ok {
		t.Fatalf("expected outermost transport to be *httputils.CallerIDTransport, got %T", rt)
	}
	retryRT, ok := callerRT.Base.(*retry.Transport)
	if !ok {
		t.Fatalf("expected CallerIDTransport.Base to be *retry.Transport, got %T", callerRT.Base)
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
