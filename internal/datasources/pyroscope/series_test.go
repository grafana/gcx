package pyroscope //nolint:testpackage // white-box test of unexported resolveMetricsStepSeconds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestResolveMetricsStepSeconds(t *testing.T) {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)

	t.Run("uses datasource minStep when step omitted", func(t *testing.T) {
		restCfg, calls := pyroscopeMinStepRESTConfig(t, "1m")

		step, err := resolveMetricsStepSeconds(context.Background(), restCfg, "pyro-1", false, start, end, 0)

		require.NoError(t, err)
		assert.InDelta(t, 60.0, step, 1e-9)
		assert.Equal(t, 1, *calls)
	})

	t.Run("explicit step wins over datasource minStep", func(t *testing.T) {
		restCfg, calls := pyroscopeMinStepRESTConfig(t, "1m")

		step, err := resolveMetricsStepSeconds(context.Background(), restCfg, "pyro-1", false, start, end, 15*time.Second)

		require.NoError(t, err)
		assert.InDelta(t, 15.0, step, 1e-9)
		assert.Equal(t, 0, *calls)
	})

	t.Run("top uses full range and does not fetch datasource config", func(t *testing.T) {
		restCfg, calls := pyroscopeMinStepRESTConfig(t, "1m")

		step, err := resolveMetricsStepSeconds(context.Background(), restCfg, "pyro-1", true, start, end, 0)

		require.NoError(t, err)
		assert.InDelta(t, 7200.0, step, 1e-9)
		assert.Equal(t, 0, *calls)
	})
}

func pyroscopeMinStepRESTConfig(t *testing.T, minStep string) (config.NamespacedRESTConfig, *int) {
	t.Helper()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/uid/pyro-1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"uid":  "pyro-1",
			"type": "grafana-pyroscope-datasource",
			"jsonData": map[string]any{
				"minStep": minStep,
			},
		}))
	}))
	t.Cleanup(srv.Close)

	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}, &calls
}
