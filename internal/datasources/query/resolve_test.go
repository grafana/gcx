package query_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestResolveDatasource(t *testing.T) {
	t.Run("explicit flag takes precedence", func(t *testing.T) {
		resolved, err := dsquery.ResolveDatasource(
			context.Background(),
			"explicit-uid",
			&config.Context{Datasources: map[string]string{"tempo": "config-uid"}},
			config.NamespacedRESTConfig{},
			"tempo",
		)
		require.NoError(t, err)
		assert.Equal(t, "explicit-uid", resolved.UID)
		assert.False(t, resolved.Persist)
	})

	t.Run("config fallback is used before auto-discovery", func(t *testing.T) {
		resolved, err := dsquery.ResolveDatasource(
			context.Background(),
			"",
			&config.Context{Datasources: map[string]string{"tempo": "config-uid"}},
			config.NamespacedRESTConfig{},
			"tempo",
		)
		require.NoError(t, err)
		assert.Equal(t, "config-uid", resolved.UID)
		assert.False(t, resolved.Persist)
	})

	t.Run("auto-discovers single matching datasource", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "prom-1", "name": "metrics", "type": "prometheus"},
			{"uid": "tempo-1", "name": "traces", "type": "tempo"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", nil, restCfg, "tempo")
		require.NoError(t, err)
		assert.Equal(t, "tempo-1", resolved.UID)
		assert.False(t, resolved.Persist)
	})

	t.Run("auto-discovers canonical cloud datasource when cloud.stack is configured", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "tempo-1", "name": "Tempo Ops", "type": "tempo"},
			{"uid": "grafanacloud-traces", "name": "grafanacloud-ops-traces", "type": "tempo"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", &config.Context{
			Cloud: &config.CloudConfig{Stack: "ops"},
		}, restCfg, "tempo")
		require.NoError(t, err)
		assert.Equal(t, "grafanacloud-traces", resolved.UID)
		assert.True(t, resolved.Persist)
	})

	t.Run("auto-discovers canonical cloud datasource when grafana.server implies stack slug", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "tempo-1", "name": "Tempo Ops", "type": "tempo"},
			{"uid": "grafanacloud-traces", "name": "grafanacloud-ops-traces", "type": "tempo"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", &config.Context{
			Grafana: &config.GrafanaConfig{Server: "https://ops.grafana.net"},
		}, restCfg, "tempo")
		require.NoError(t, err)
		assert.Equal(t, "grafanacloud-traces", resolved.UID)
		assert.True(t, resolved.Persist)
	})

	t.Run("auto-discovers canonical cloud datasource when GRAFANA_CLOUD_STACK is set", func(t *testing.T) {
		t.Setenv("GRAFANA_CLOUD_STACK", "ops")

		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "tempo-1", "name": "Tempo Ops", "type": "tempo"},
			{"uid": "grafanacloud-traces", "name": "grafanacloud-ops-traces", "type": "tempo"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", nil, restCfg, "tempo")
		require.NoError(t, err)
		assert.Equal(t, "grafanacloud-traces", resolved.UID)
		assert.True(t, resolved.Persist)
	})

	t.Run("auto-discovers pyroscope datasource via normalized plugin id", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "pyro-1", "name": "profiles", "type": "grafana-pyroscope-datasource"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", nil, restCfg, "pyroscope")
		require.NoError(t, err)
		assert.Equal(t, "pyro-1", resolved.UID)
		assert.False(t, resolved.Persist)
	})

	t.Run("errors when no matching datasource exists", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "prom-1", "name": "metrics", "type": "prometheus"},
		})

		_, err := dsquery.ResolveDatasource(context.Background(), "", nil, restCfg, "tempo")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no tempo datasource found in Grafana")
	})

	t.Run("errors when multiple matching datasources are ambiguous and cloud.stack is unset", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "tempo-1", "name": "traces-a", "type": "tempo"},
			{"uid": "tempo-2", "name": "traces-b", "type": "tempo"},
		})

		_, err := dsquery.ResolveDatasource(context.Background(), "", nil, restCfg, "tempo")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "multiple tempo datasources found")
		assert.Contains(t, err.Error(), "set cloud.stack or grafana.server to enable auto-discovery")
		assert.Contains(t, err.Error(), "traces-a (tempo-1)")
		assert.Contains(t, err.Error(), "traces-b (tempo-2)")
	})
}

func testDatasourceRESTConfig(t *testing.T, payload any) config.NamespacedRESTConfig {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/datasources", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(payload))
	}))
	t.Cleanup(srv.Close)

	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
}
