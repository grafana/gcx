package investigations_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrg/xdg"
	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// isolateCapabilityCache redirects XDG_STATE_HOME to t.TempDir so the cache
// file used by capability detection is scoped per test. adrg/xdg caches paths
// at package init, so we must Reload after setting the env var.
func isolateCapabilityCache(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })
	return dir
}

func newCapabilityClient(t *testing.T, handler http.Handler) (*assistanthttp.Client, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	c, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return c, server.URL
}

func TestDetectCapability_Probe(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantV2  bool
		wantErr bool
	}{
		{name: "v2 stack returns 200", status: http.StatusOK, wantV2: true},
		{name: "v1 stack returns 404", status: http.StatusNotFound, wantV2: false},
		{name: "transport error", status: http.StatusInternalServerError, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateCapabilityCache(t)
			t.Setenv("GCX_ASSISTANT_API_VERSION", "")

			var calls int
			client, host := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Contains(t, r.URL.Path, "/investigations/lodestone/profiles")
				if tt.status != http.StatusOK {
					w.WriteHeader(tt.status)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"data":{"profiles":[]}}`))
			}))

			c, err := investigations.DetectCapability(t.Context(), client, host)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantV2, c.V2)
			assert.Equal(t, 1, calls, "expected exactly one probe call")
		})
	}
}

func TestDetectCapability_CachedWithinTTL(t *testing.T) {
	isolateCapabilityCache(t)
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")

	var calls int
	client, host := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	first, err := investigations.DetectCapability(t.Context(), client, host)
	require.NoError(t, err)
	require.True(t, first.V2)

	second, err := investigations.DetectCapability(t.Context(), client, host)
	require.NoError(t, err)
	require.True(t, second.V2)

	assert.Equal(t, 1, calls, "second call should hit the cache")
}

func TestDetectCapability_EnvOverride(t *testing.T) {
	isolateCapabilityCache(t)

	calls := 0
	client, host := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))

	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	c, err := investigations.DetectCapability(t.Context(), client, host)
	require.NoError(t, err)
	assert.True(t, c.V2)

	t.Setenv("GCX_ASSISTANT_API_VERSION", "v1")
	c, err = investigations.DetectCapability(t.Context(), client, host)
	require.NoError(t, err)
	assert.False(t, c.V2)

	assert.Equal(t, 0, calls, "env override should bypass the probe")
}

func TestCapabilityCachePath_HonoursXDG(t *testing.T) {
	dir := isolateCapabilityCache(t)
	got := investigations.CapabilityCachePath()
	assert.Equal(t, filepath.Join(dir, "gcx", "lodestone-capability.json"), got)
}

func TestDetectCapability_RefreshesPastTTL(t *testing.T) {
	dir := isolateCapabilityCache(t)
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")

	// Seed a stale cache entry (older than 24h).
	staleHost := "http://example"
	stale := []byte(`{"entries":{"` + staleHost + `":{"v2":false,"checkedAt":"` +
		time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano) + `"}}}`)
	cachePath := filepath.Join(dir, "gcx", "lodestone-capability.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(cachePath), 0o755))
	require.NoError(t, os.WriteFile(cachePath, stale, 0o600))

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// Probe must run against a real HTTP server; the cache key remains
	// staleHost so the seeded entry is found and judged expired.
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	client, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)

	c, err := investigations.DetectCapability(t.Context(), client, staleHost)
	require.NoError(t, err)
	assert.True(t, c.V2)
	assert.Equal(t, 1, calls, "stale entry should trigger a refresh")
}
