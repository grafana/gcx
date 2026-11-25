package config_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverStackID_Success(t *testing.T) {
	var pathCheck string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathCheck = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-12345",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	stackID, err := config.DiscoverStackID(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "/bootdata", pathCheck)
	assert.Equal(t, int64(12345), stackID)
}

func TestDiscoverStackID_NonStackNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "grafana",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	_, err := config.DiscoverStackID(t.Context(), cfg)
	assert.Error(t, err)
}

func TestDiscoverStackID_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	_, err := config.DiscoverStackID(t.Context(), cfg)
	assert.Error(t, err)
}

func TestDiscoverStackID_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{Server: server.URL}

	_, err := config.DiscoverStackID(t.Context(), cfg)
	assert.Error(t, err)
}

func TestDiscoverStackID_TLSSkipVerify(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"settings": map[string]any{
				"namespace": "stacks-678",
			},
		})
	}))
	defer server.Close()

	cfg := config.GrafanaConfig{
		Server: server.URL,
		TLS: &config.TLS{
			Insecure: true,
		},
	}

	stackID, err := config.DiscoverStackID(t.Context(), cfg)
	require.NoError(t, err)
	assert.Equal(t, int64(678), stackID)
}

const whoamiTestPath = "/apis/iam.grafana.app/v0alpha1/whoami"

// discoveryServer records discovery hits and answers whoami/bootdata with the
// supplied handlers (nil -> 404).
type discoveryServer struct {
	whoamiHits, bootdataHits, otherHits int
	authHeader                          string
}

func newDiscoveryServer(t *testing.T, whoami, bootdata http.HandlerFunc) (*httptest.Server, *discoveryServer) {
	t.Helper()
	rec := &discoveryServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, whoamiTestPath):
			rec.whoamiHits++
			rec.authHeader = r.Header.Get("Authorization")
			if whoami == nil {
				http.NotFound(w, r)
				return
			}
			whoami(w, r)
		case r.URL.Path == "/bootdata":
			rec.bootdataHits++
			if bootdata == nil {
				http.NotFound(w, r)
				return
			}
			bootdata(w, r)
		default:
			rec.otherHits++
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func whoamiNamespace(ns string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"identity":  map[string]any{"type": "user", "name": "u1"},
			"namespace": ns,
		})
	}
}

func bootdataNamespace(ns string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"settings": map[string]any{"namespace": ns}})
	}
}

func TestNamespaceDiscovery_Whoami(t *testing.T) {
	tests := []struct {
		name          string
		whoami        http.HandlerFunc
		bootdata      http.HandlerFunc
		wantNamespace string
		wantBootdata  int
	}{
		{
			name:          "cloud stack from whoami skips bootdata",
			whoami:        whoamiNamespace("stacks-31701"),
			bootdata:      bootdataNamespace("stacks-99999"),
			wantNamespace: "stacks-31701",
			wantBootdata:  0,
		},
		{
			name:          "non-stack whoami namespace falls back to bootdata",
			whoami:        whoamiNamespace("default"),
			bootdata:      bootdataNamespace("stacks-99999"),
			wantNamespace: "stacks-99999",
			wantBootdata:  1,
		},
		{
			name:          "whoami unavailable falls back to bootdata",
			whoami:        nil,
			bootdata:      bootdataNamespace("stacks-4242"),
			wantNamespace: "stacks-4242",
			wantBootdata:  1,
		},
		{
			name:          "whoami with empty namespace falls back to bootdata",
			whoami:        whoamiNamespace(""),
			bootdata:      bootdataNamespace("stacks-4343"),
			wantNamespace: "stacks-4343",
			wantBootdata:  1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := newDiscoveryServer(t, tc.whoami, tc.bootdata)
			ctx := config.Context{
				Name:    "test",
				Grafana: &config.GrafanaConfig{Server: srv.URL, APIToken: "tok", OrgID: 1},
			}

			restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
			require.NoError(t, err)
			assert.Equal(t, tc.wantNamespace, restCfg.Namespace)
			assert.Equal(t, 1, rec.whoamiHits)
			assert.Equal(t, "Bearer tok", rec.authHeader)
			assert.Equal(t, tc.wantBootdata, rec.bootdataHits)
			assert.Zero(t, rec.otherHits)
		})
	}
}

func TestNamespaceDiscovery_BasicAuth(t *testing.T) {
	srv, rec := newDiscoveryServer(t, whoamiNamespace("stacks-5"), nil)
	ctx := config.Context{
		Name:    "test",
		Grafana: &config.GrafanaConfig{Server: srv.URL, User: "admin", Password: "pw"},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	require.NoError(t, err)
	assert.Equal(t, "stacks-5", restCfg.Namespace)
	assert.True(t, strings.HasPrefix(rec.authHeader, "Basic "), "got %q", rec.authHeader)
}

// OAuth discovery goes through the assistant proxy with the current access
// token and must never trigger a refresh: persistence hooks are not wired yet
// when the REST config is built.
func TestNamespaceDiscovery_OAuthProxyDoesNotRefresh(t *testing.T) {
	var refreshHits int
	var whoamiPath, authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/cli/v1/auth/refresh":
			refreshHits++
			http.Error(w, "refresh not expected", http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, whoamiTestPath):
			whoamiPath = r.URL.Path
			authHeader = r.Header.Get("Authorization")
			whoamiNamespace("stacks-31701")(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	ctx := config.Context{
		Name: "test",
		Grafana: &config.GrafanaConfig{
			Server:              "https://unused.example.invalid",
			ProxyEndpoint:       srv.URL,
			OAuthToken:          "gat_current",
			OAuthRefreshToken:   "gar_current",
			OAuthTokenExpiresAt: "2020-01-01T00:00:00Z", // expired: a refreshing client would rotate it
		},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	require.NoError(t, err)
	assert.Equal(t, "stacks-31701", restCfg.Namespace)
	assert.Equal(t, "/api/cli/v1/proxy"+whoamiTestPath, whoamiPath)
	assert.Equal(t, "Bearer gat_current", authHeader)
	assert.Zero(t, refreshHits)
}

func TestGrafanaConfig_Validate_UsesWhoami(t *testing.T) {
	srv, rec := newDiscoveryServer(t, whoamiNamespace("stacks-777"), nil)
	cfg := config.GrafanaConfig{Server: srv.URL, APIToken: "tok"}

	require.NoError(t, cfg.Validate(t.Context(), "ctx"))
	assert.Equal(t, 1, rec.whoamiHits)
	assert.Zero(t, rec.bootdataHits)
}

// whoami answers anonymous callers with the stack namespace too, so discovery
// works without credentials and never needs /bootdata.
func TestNamespaceDiscovery_AnonymousWhoami(t *testing.T) {
	anonymous := func(w http.ResponseWriter, _ *http.Request) {
		// Shape captured from a real anonymous whoami response.
		_, _ = w.Write([]byte(`{"identity":{"type":"anonymous","name":""},"displayName":"Anonymous","namespace":"stacks-35611","role":"Viewer"}`))
	}
	srv, rec := newDiscoveryServer(t, anonymous, nil)
	ctx := config.Context{Name: "test", Grafana: &config.GrafanaConfig{Server: srv.URL}}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	require.NoError(t, err)
	assert.Equal(t, "stacks-35611", restCfg.Namespace)
	assert.Empty(t, rec.authHeader)
	assert.Zero(t, rec.bootdataHits)
}

// dropConnection closes the connection without a response, simulating a
// network-level failure.
func dropConnection(w http.ResponseWriter, _ *http.Request) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		panic("response writer does not support hijacking")
	}
	conn, _, err := hj.Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func TestNamespaceDiscovery_UnreachableWhoamiSkipsBootdata(t *testing.T) {
	srv, rec := newDiscoveryServer(t, dropConnection, bootdataNamespace("stacks-1"))
	ctx := config.Context{
		Name:    "test",
		Grafana: &config.GrafanaConfig{Server: srv.URL, APIToken: "tok", OrgID: 1},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	require.NoError(t, err)
	assert.Equal(t, "default", restCfg.Namespace)
	assert.Equal(t, 1, rec.whoamiHits)
	assert.Zero(t, rec.bootdataHits)
}

// In OAuth mode whoami goes through the proxy, so an unreachable proxy must
// not stop /bootdata on the Grafana server itself.
func TestNamespaceDiscovery_UnreachableOAuthProxyFallsBackToBootdata(t *testing.T) {
	srv, rec := newDiscoveryServer(t, nil, bootdataNamespace("stacks-4545"))
	proxy := httptest.NewServer(http.HandlerFunc(dropConnection))
	t.Cleanup(proxy.Close)

	ctx := config.Context{
		Name: "test",
		Grafana: &config.GrafanaConfig{
			Server:        srv.URL,
			ProxyEndpoint: proxy.URL,
			OAuthToken:    "gat_current",
		},
	}

	restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
	require.NoError(t, err)
	assert.Equal(t, "stacks-4545", restCfg.Namespace)
	assert.Equal(t, 1, rec.bootdataHits)
}

func TestNamespaceDiscovery_NotCloudStackCache(t *testing.T) {
	unavailable := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}
	tests := []struct {
		name         string
		whoami       http.HandlerFunc
		bootdata     http.HandlerFunc
		wantRequests int // per probe, across both lookups
	}{
		{
			name:         "on-prem answer is memoized",
			whoami:       whoamiNamespace("default"),
			bootdata:     bootdataNamespace(""),
			wantRequests: 1,
		},
		{
			name:         "missing endpoints are memoized",
			whoami:       nil,
			bootdata:     nil,
			wantRequests: 1,
		},
		{
			name:         "transient bootdata failure is not memoized",
			whoami:       whoamiNamespace("default"),
			bootdata:     unavailable,
			wantRequests: 2,
		},
		{
			name:         "transient whoami failure is not memoized",
			whoami:       unavailable,
			bootdata:     nil,
			wantRequests: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := newDiscoveryServer(t, tc.whoami, tc.bootdata)
			ctx := config.Context{
				Name:    "test",
				Grafana: &config.GrafanaConfig{Server: srv.URL, APIToken: "tok", OrgID: 1},
			}

			for range 2 {
				restCfg, err := config.NewNamespacedRESTConfig(t.Context(), ctx)
				require.NoError(t, err)
				assert.Equal(t, "default", restCfg.Namespace)
			}
			assert.Equal(t, tc.wantRequests, rec.whoamiHits)
			assert.Equal(t, tc.wantRequests, rec.bootdataHits)
		})
	}
}
