package loki_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/loki"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsExploreURL(t *testing.T) {
	t.Run("builds logs explore link", func(t *testing.T) {
		got := loki.LogsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{
			DatasourceUID:  "grafanacloud-logs",
			DatasourceType: "loki",
			Expr:           `{name="ingress-nginx-controller"}`,
			OrgID:          1,
		})

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "1", params.Get("orgId"))
		assert.Contains(t, params.Get("panes"), `"datasource":"grafanacloud-logs"`)
		assert.Contains(t, params.Get("panes"), `"expr":"{name=\"ingress-nginx-controller\"}"`)
		assert.Contains(t, params.Get("panes"), `"queryType":"range"`)
		assert.Contains(t, params.Get("panes"), `"direction":"backward"`)
		assert.Contains(t, params.Get("panes"), `"sortOrder":"Descending"`)
		assert.Contains(t, params.Get("panes"), `"compact":false`)
		assert.Contains(t, params.Get("panes"), `"from":"now-1m"`)
		assert.Contains(t, params.Get("panes"), `"to":"now"`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, loki.LogsExploreURL("", dsquery.ExploreQuery{DatasourceUID: "loki-uid", Expr: "{}"}))
		assert.Empty(t, loki.LogsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{Expr: "{}"}))
		assert.Empty(t, loki.LogsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{DatasourceUID: "loki-uid"}))
	})
}

func TestMetricsExploreURL(t *testing.T) {
	t.Run("builds metric logql explore link", func(t *testing.T) {
		got := loki.MetricsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{
			DatasourceUID:  "grafanacloud-logs",
			DatasourceType: "loki",
			Expr:           `rate({name="ingress-nginx-controller"}[$__auto])`,
			Instant:        false,
		})

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Contains(t, params.Get("panes"), `"datasource":"grafanacloud-logs"`)
		assert.Contains(t, params.Get("panes"), `"expr":"rate({name=\"ingress-nginx-controller\"}[$__auto])"`)
		assert.Contains(t, params.Get("panes"), `"queryType":"range"`)
		assert.Contains(t, params.Get("panes"), `"instant":false`)
		assert.Contains(t, params.Get("panes"), `"range":true`)
		assert.Contains(t, params.Get("panes"), `"direction":"backward"`)
		assert.Contains(t, params.Get("panes"), `"compact":false`)
		assert.Contains(t, params.Get("panes"), `"from":"now-1m"`)
		assert.Contains(t, params.Get("panes"), `"to":"now"`)
		assert.NotContains(t, params.Get("panes"), `"panelsState"`)
	})

	t.Run("builds instant metric logql explore link", func(t *testing.T) {
		got := loki.MetricsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{
			DatasourceUID:  "grafanacloud-logs",
			DatasourceType: "loki",
			Expr:           `rate({name="ingress-nginx-controller"}[$__auto])`,
			Instant:        true,
		})

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Contains(t, params.Get("panes"), `"instant":true`)
		assert.Contains(t, params.Get("panes"), `"range":false`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, loki.MetricsExploreURL("", dsquery.ExploreQuery{DatasourceUID: "loki-uid", Expr: "rate({}[5m])"}))
		assert.Empty(t, loki.MetricsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{Expr: "rate({}[5m])"}))
		assert.Empty(t, loki.MetricsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{DatasourceUID: "loki-uid"}))
	})
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
