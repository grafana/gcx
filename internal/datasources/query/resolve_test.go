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

	t.Run("auto-discovers canonical cloud datasource when the stack slug is configured", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "tempo-1", "name": "Tempo Ops", "type": "tempo"},
			{"uid": "grafanacloud-traces", "name": "grafanacloud-ops-traces", "type": "tempo"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", &config.Context{
			StackEntry: &config.StackConfig{Slug: "ops"},
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

	t.Run("auto-discovers via GRAFANA_SERVER env when no config exists", func(t *testing.T) {
		t.Setenv("GRAFANA_SERVER", "https://ops.grafana.net")

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

	t.Run("errors when multiple matching datasources are ambiguous and no stack slug is set", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "tempo-1", "name": "traces-a", "type": "tempo"},
			{"uid": "tempo-2", "name": "traces-b", "type": "tempo"},
		})

		_, err := dsquery.ResolveDatasource(context.Background(), "", nil, restCfg, "tempo")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "multiple tempo datasources found")
		assert.Contains(t, err.Error(), "set the stack slug (gcx config set stacks.<name>.slug <slug>) or grafana.server to enable auto-discovery")
		assert.Contains(t, err.Error(), "traces-a (tempo-1)")
		assert.Contains(t, err.Error(), "traces-b (tempo-2)")
	})
}

func TestResolveDatasource_TypePopulated(t *testing.T) {
	t.Run("single match populates Type", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "loki-1", "name": "logs", "type": "loki"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", nil, restCfg, "loki")
		require.NoError(t, err)
		assert.Equal(t, "loki-1", resolved.UID)
		assert.Equal(t, "loki", resolved.Type)
	})

	t.Run("canonical cloud match populates Type", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "prom-1", "name": "other-prom", "type": "prometheus"},
			{"uid": "prom-cloud", "name": "grafanacloud-ops-prom", "type": "prometheus"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", &config.Context{
			StackEntry: &config.StackConfig{Slug: "ops"},
		}, restCfg, "prometheus")
		require.NoError(t, err)
		assert.Equal(t, "prom-cloud", resolved.UID)
		assert.Equal(t, "prometheus", resolved.Type)
	})

	t.Run("explicit flag does not populate Type", func(t *testing.T) {
		resolved, err := dsquery.ResolveDatasource(
			context.Background(),
			"explicit-uid",
			nil,
			config.NamespacedRESTConfig{},
			"loki",
		)
		require.NoError(t, err)
		assert.Equal(t, "explicit-uid", resolved.UID)
		assert.Empty(t, resolved.Type)
	})

	t.Run("pyroscope plugin id preserved in Type", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "pyro-1", "name": "profiles", "type": "grafana-pyroscope-datasource"},
		})

		resolved, err := dsquery.ResolveDatasource(context.Background(), "", nil, restCfg, "pyroscope")
		require.NoError(t, err)
		assert.Equal(t, "pyro-1", resolved.UID)
		assert.Equal(t, "grafana-pyroscope-datasource", resolved.Type)
	})
}

func TestResolveValidateAndSaveDatasource(t *testing.T) {
	t.Run("skips GET when type already known from discovery", func(t *testing.T) {
		var apiCalls int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"uid": "loki-1", "name": "logs", "type": "loki"},
			})
		}))
		t.Cleanup(srv.Close)

		restCfg := config.NamespacedRESTConfig{
			Config:    rest.Config{Host: srv.URL},
			Namespace: "default",
		}

		uid, dsType, err := dsquery.ResolveValidateAndSaveDatasource(context.Background(), nil, "", nil, restCfg, "loki")
		require.NoError(t, err)
		assert.Equal(t, "loki-1", uid)
		assert.Equal(t, "loki", dsType)
		assert.Equal(t, 1, apiCalls, "should only call /api/datasources (List), not /api/datasources/uid/*")
	})

	t.Run("fetches type when UID from flag", func(t *testing.T) {
		var paths []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uid": "prom-1", "name": "metrics", "type": "prometheus",
			})
		}))
		t.Cleanup(srv.Close)

		restCfg := config.NamespacedRESTConfig{
			Config:    rest.Config{Host: srv.URL},
			Namespace: "default",
		}

		uid, dsType, err := dsquery.ResolveValidateAndSaveDatasource(context.Background(), nil, "prom-1", nil, restCfg, "prometheus")
		require.NoError(t, err)
		assert.Equal(t, "prom-1", uid)
		assert.Equal(t, "prometheus", dsType)
		require.Len(t, paths, 1)
		assert.Equal(t, "/api/datasources/uid/prom-1", paths[0])
	})

	t.Run("returns error on type mismatch", func(t *testing.T) {
		restCfg := testDatasourceRESTConfig(t, []map[string]any{
			{"uid": "prom-1", "name": "metrics", "type": "prometheus"},
		})

		_, _, err := dsquery.ResolveValidateAndSaveDatasource(context.Background(), nil, "", nil, restCfg, "loki")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no loki datasource found")
	})

	t.Run("returns error on type mismatch from flag path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"uid": "prom-1", "name": "metrics", "type": "prometheus",
			})
		}))
		t.Cleanup(srv.Close)

		restCfg := config.NamespacedRESTConfig{
			Config:    rest.Config{Host: srv.URL},
			Namespace: "default",
		}

		_, _, err := dsquery.ResolveValidateAndSaveDatasource(context.Background(), nil, "prom-1", nil, restCfg, "loki")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "datasource is type prometheus, not loki")
	})
}

func TestNormalizeKind(t *testing.T) {
	tests := []struct {
		pluginID string
		want     string
	}{
		{"prometheus", "prometheus"},
		{"loki", "loki"},
		{"tempo", "tempo"},
		{"grafana-pyroscope-datasource", "pyroscope"},
		{"grafana-clickhouse-datasource", "clickhouse"},
		{"synthetic-monitoring-datasource", "synthetic-monitoring"},
		{"grafana-amazonprometheus-datasource", "prometheus"},
		{"grafana-azureprometheus-datasource", "prometheus"},
		{"cloudwatch", "cloudwatch"},
		{"unknown-datasource", "unknown-datasource"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.pluginID, func(t *testing.T) {
			assert.Equal(t, tt.want, dsquery.NormalizeKind(tt.pluginID))
		})
	}
}

func testDatasourceRESTConfig(t *testing.T, payload any) config.NamespacedRESTConfig {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(payload))
	}))
	t.Cleanup(srv.Close)

	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
}
