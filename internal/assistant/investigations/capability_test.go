package investigations_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
			t.Setenv("GCX_ASSISTANT_API_VERSION", "")

			var calls int
			client := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Contains(t, r.URL.Path, "/investigations/lodestone")
				assert.Equal(t, "1", r.URL.Query().Get("limit"))
				if tt.status != http.StatusOK {
					w.WriteHeader(tt.status)
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"data":{"investigations":[]}}`))
			}))

			loader := newCapabilityLoader(t)
			c, err := investigations.DetectCapability(context.Background(), loader, client)
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

func TestDetectCapability_Cached(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")

	var calls int
	client := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	loader := newCapabilityLoader(t)

	first, err := investigations.DetectCapability(context.Background(), loader, client)
	require.NoError(t, err)
	require.True(t, first.V2)

	second, err := investigations.DetectCapability(context.Background(), loader, client)
	require.NoError(t, err)
	require.True(t, second.V2)

	assert.Equal(t, 1, calls, "second call should hit the cache")
}

func TestDetectCapability_EnvOverride(t *testing.T) {
	calls := 0
	client := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	loader := newCapabilityLoader(t)

	t.Setenv("GCX_ASSISTANT_API_VERSION", "v2")
	c, err := investigations.DetectCapability(context.Background(), loader, client)
	require.NoError(t, err)
	assert.True(t, c.V2)

	t.Setenv("GCX_ASSISTANT_API_VERSION", "v1")
	c, err = investigations.DetectCapability(context.Background(), loader, client)
	require.NoError(t, err)
	assert.False(t, c.V2)

	assert.Equal(t, 0, calls, "env override should bypass the probe")
}

// TestCachedV2_HonoursPersistedConfig verifies CachedV2 reads back the value
// that DetectCapability persisted via SaveProviderConfig.
func TestCachedV2_HonoursPersistedConfig(t *testing.T) {
	t.Setenv("GCX_ASSISTANT_API_VERSION", "")

	client := newCapabilityClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	loader := newCapabilityLoader(t)

	assert.False(t, investigations.CachedV2(context.Background(), loader), "no cache yet → false")

	_, err := investigations.DetectCapability(context.Background(), loader, client)
	require.NoError(t, err)

	assert.True(t, investigations.CachedV2(context.Background(), loader), "cache populated → true")
}
