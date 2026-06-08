package investigations_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/assistant/investigations"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// newCapabilityLoader builds a ConfigLoader pointed at a fresh per-test gcx
// config file so SaveProviderConfig persistence is isolated.
func newCapabilityLoader(t *testing.T) *providers.ConfigLoader {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte("contexts:\n  default: {}\ncurrent-context: default\n"), 0o600))
	l := &providers.ConfigLoader{}
	l.SetConfigFile(cfgFile)
	return l
}

func newCapabilityClient(t *testing.T, handler http.Handler) *assistanthttp.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	c, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return c
}

// modeProbeHandler simulates a stack that returns 200 only for the paths
// listed in `okPaths`. Used to fake each rung of the probe.
func modeProbeHandler(okPaths ...string) (http.Handler, *int) {
	calls := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if slices.Contains(okPaths, r.URL.Path) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"investigations":[]}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}), &calls
}

func TestDetectAPIMode_Probe(t *testing.T) {
	tests := []struct {
		name     string
		okPaths  []string
		wantMode investigations.APIMode
		wantHits int
	}{
		{
			name:     "v2-standard stack",
			okPaths:  []string{"/api/plugins/grafana-assistant-app/resources/api/v2/investigations"},
			wantMode: investigations.APIModeV2Standard,
			wantHits: 1,
		},
		{
			name:     "legacy v1 only",
			okPaths:  nil,
			wantMode: investigations.APIModeLegacy,
			wantHits: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GCX_ASSISTANT_API_VERSION", "")

			handler, calls := modeProbeHandler(tt.okPaths...)
			client := newCapabilityClient(t, handler)
			loader := newCapabilityLoader(t)

			mode, err := investigations.DetectAPIMode(context.Background(), loader, client)
			require.NoError(t, err)
			assert.Equal(t, tt.wantMode, mode)
			assert.Equal(t, tt.wantHits, *calls)
		})
	}
}

func TestDetectAPIMode_ProbeTransportError(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")
	client := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	loader := newCapabilityLoader(t)
	_, err := investigations.DetectAPIMode(context.Background(), loader, client)
	require.Error(t, err)
}

func TestDetectAPIMode_Cached(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")

	handler, calls := modeProbeHandler("/api/plugins/grafana-assistant-app/resources/api/v2/investigations")
	client := newCapabilityClient(t, handler)
	loader := newCapabilityLoader(t)

	first, err := investigations.DetectAPIMode(context.Background(), loader, client)
	require.NoError(t, err)
	require.Equal(t, investigations.APIModeV2Standard, first)

	second, err := investigations.DetectAPIMode(context.Background(), loader, client)
	require.NoError(t, err)
	require.Equal(t, investigations.APIModeV2Standard, second)

	assert.Equal(t, 1, *calls, "second call should hit the cache")
}

func TestDetectAPIMode_EnvOverride(t *testing.T) {
	calls := 0
	client := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	loader := newCapabilityLoader(t)

	cases := []struct {
		env  string
		want investigations.APIMode
	}{
		{"v2", investigations.APIModeV2Standard},
		{"v1", investigations.APIModeLegacy},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("GCX_ASSISTANT_API_VERSION", tc.env)
			m, err := investigations.DetectAPIMode(context.Background(), loader, client)
			require.NoError(t, err)
			assert.Equal(t, tc.want, m)
		})
	}
	assert.Equal(t, 0, calls, "env override should bypass the probe")
}

// TestCachedAPIMode_HonoursPersistedConfig verifies CachedAPIMode reads back
// the value that DetectAPIMode persisted via SaveProviderConfig.
func TestCachedAPIMode_HonoursPersistedConfig(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")

	handler, _ := modeProbeHandler("/api/plugins/grafana-assistant-app/resources/api/v2/investigations")
	client := newCapabilityClient(t, handler)
	loader := newCapabilityLoader(t)

	assert.Equal(t, investigations.APIModeLegacy, investigations.CachedAPIMode(context.Background(), loader),
		"no cache yet → legacy default")

	_, err := investigations.DetectAPIMode(context.Background(), loader, client)
	require.NoError(t, err)

	assert.Equal(t, investigations.APIModeV2Standard, investigations.CachedAPIMode(context.Background(), loader),
		"cache populated → v2-standard")
}

// TestDetectAPIMode_V1NotCached verifies that a v1 probe result is never
// persisted, so a stack upgraded to Lodestone after the first probe is
// re-detected instead of being permanently stranded on legacy endpoints.
func TestDetectAPIMode_V1NotCached(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")

	v2Path := "/api/plugins/grafana-assistant-app/resources/api/v2/investigations"
	upgraded := false
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if upgraded && r.URL.Path == v2Path {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"investigations":[]}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	client := newCapabilityClient(t, handler)
	loader := newCapabilityLoader(t)

	// First probe: stack is still v1. Result must not be cached.
	mode, err := investigations.DetectAPIMode(context.Background(), loader, client)
	require.NoError(t, err)
	assert.Equal(t, investigations.APIModeLegacy, mode)
	assert.Equal(t, investigations.APIModeLegacy, investigations.CachedAPIMode(context.Background(), loader),
		"v1 must not be persisted")

	// Stack upgrades to Lodestone; the next probe must pick it up.
	upgraded = true
	mode, err = investigations.DetectAPIMode(context.Background(), loader, client)
	require.NoError(t, err)
	assert.Equal(t, investigations.APIModeV2Standard, mode, "re-probe after upgrade detects v2")

	// v2 is sticky from here on.
	callsAfterV2 := calls
	mode, err = investigations.DetectAPIMode(context.Background(), loader, client)
	require.NoError(t, err)
	assert.Equal(t, investigations.APIModeV2Standard, mode)
	assert.Equal(t, callsAfterV2, calls, "v2 result should be served from cache")
}

func TestAPIMode_SupportsV2(t *testing.T) {
	assert.False(t, investigations.APIModeLegacy.SupportsV2())
	assert.True(t, investigations.APIModeV2Standard.SupportsV2())
}

// TestDetectAPIMode_StaleLodestoneCache verifies that an api-mode of
// "lodestone" persisted by an older gcx version is treated as a cache miss,
// triggering a fresh probe that converges on v2.
func TestDetectAPIMode_StaleLodestoneCache(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")

	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfgFile, []byte(
		"contexts:\n"+
			"  default:\n"+
			"    providers:\n"+
			"      assistant:\n"+
			"        api-mode: lodestone\n"+
			"current-context: default\n",
	), 0o600))
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	handler, calls := modeProbeHandler("/api/plugins/grafana-assistant-app/resources/api/v2/investigations")
	client := newCapabilityClient(t, handler)

	mode, err := investigations.DetectAPIMode(context.Background(), loader, client)
	require.NoError(t, err)
	assert.Equal(t, investigations.APIModeV2Standard, mode)
	assert.Equal(t, 1, *calls, "stale lodestone cache should trigger a re-probe")

	mode2, err := investigations.DetectAPIMode(context.Background(), loader, client)
	require.NoError(t, err)
	assert.Equal(t, investigations.APIModeV2Standard, mode2)
	assert.Equal(t, 1, *calls, "second call should hit the rewritten cache")
}
