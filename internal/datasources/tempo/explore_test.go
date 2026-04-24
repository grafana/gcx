package tempo_test

import (
	"net/url"
	"testing"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchExploreURL(t *testing.T) {
	t.Run("builds traceql search explore link", func(t *testing.T) {
		got := tempo.SearchExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{
			DatasourceUID:  "grafanacloud-traces",
			DatasourceType: "tempo",
			Expr:           `{name != nil}`,
		}, 20)

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Contains(t, params.Get("panes"), `"datasource":"grafanacloud-traces"`)
		assert.Contains(t, params.Get("panes"), `"queryType":"traceql"`)
		assert.Contains(t, params.Get("panes"), `"limit":20`)
		assert.Contains(t, params.Get("panes"), `"tableType":"traces"`)
		assert.Contains(t, params.Get("panes"), `"metricsQueryType":"range"`)
		assert.Contains(t, params.Get("panes"), `"serviceMapUseNativeHistograms":false`)
		assert.Contains(t, params.Get("panes"), `"query":"{name != nil}"`)
		assert.Contains(t, params.Get("panes"), `"from":"now-1m"`)
		assert.Contains(t, params.Get("panes"), `"to":"now"`)
		assert.Contains(t, params.Get("panes"), `"compact":false`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, tempo.SearchExploreURL("", dsquery.ExploreQuery{DatasourceUID: "tempo-uid", Expr: "{}"}, 20))
		assert.Empty(t, tempo.SearchExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{Expr: "{}"}, 20))
		assert.Empty(t, tempo.SearchExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{DatasourceUID: "tempo-uid"}, 20))
	})

	t.Run("returns empty for unlimited result searches", func(t *testing.T) {
		assert.Empty(t, tempo.SearchExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{
			DatasourceUID:  "grafanacloud-traces",
			DatasourceType: "tempo",
			Expr:           `{name != nil}`,
		}, 0))
	})
}

func TestMetricsExploreURL(t *testing.T) {
	t.Run("builds traceql metrics explore link", func(t *testing.T) {
		got := tempo.MetricsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{
			DatasourceUID:  "grafanacloud-traces",
			DatasourceType: "tempo",
			Expr:           `{name != nil} | rate()`,
		}, 20)

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Contains(t, params.Get("panes"), `"datasource":"grafanacloud-traces"`)
		assert.Contains(t, params.Get("panes"), `"queryType":"traceql"`)
		assert.Contains(t, params.Get("panes"), `"limit":20`)
		assert.Contains(t, params.Get("panes"), `"tableType":"traces"`)
		assert.Contains(t, params.Get("panes"), `"metricsQueryType":"range"`)
		assert.Contains(t, params.Get("panes"), `"serviceMapUseNativeHistograms":false`)
		assert.Contains(t, params.Get("panes"), `"query":"{name != nil} | rate()"`)
		assert.Contains(t, params.Get("panes"), `"from":"now-1m"`)
		assert.Contains(t, params.Get("panes"), `"to":"now"`)
		assert.Contains(t, params.Get("panes"), `"compact":false`)
	})

	t.Run("uses instant metrics query type for instant queries", func(t *testing.T) {
		got := tempo.MetricsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{
			DatasourceUID:  "grafanacloud-traces",
			DatasourceType: "tempo",
			Expr:           `{name != nil} | rate()`,
			Instant:        true,
		}, 20)

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Contains(t, params.Get("panes"), `"metricsQueryType":"instant"`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, tempo.MetricsExploreURL("", dsquery.ExploreQuery{DatasourceUID: "tempo-uid", Expr: "{ } | rate()"}, 20))
		assert.Empty(t, tempo.MetricsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{Expr: "{ } | rate()"}, 20))
		assert.Empty(t, tempo.MetricsExploreURL("https://ops.grafana-ops.net", dsquery.ExploreQuery{DatasourceUID: "tempo-uid"}, 20))
	})
}

func TestTraceExploreURL(t *testing.T) {
	t.Run("builds trace explore link", func(t *testing.T) {
		got := tempo.TraceExploreURL("https://mystack.grafana.net/", dsquery.ExploreQuery{
			DatasourceUID:  "tempo-uid",
			DatasourceType: "tempo",
			From:           "2026-04-24T10:00:00Z",
			To:             "2026-04-24T11:00:00Z",
			OrgID:          9,
		}, "287e20fb791cf30")

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "9", params.Get("orgId"))
		assert.Equal(t, "287e20fb791cf30", params.Get("traceId"))
		assert.Contains(t, params.Get("panes"), `"datasource":"tempo-uid"`)
		assert.Contains(t, params.Get("panes"), `"queryType":"traceql"`)
		assert.Contains(t, params.Get("panes"), `"limit":20`)
		assert.Contains(t, params.Get("panes"), `"tableType":"traces"`)
		assert.Contains(t, params.Get("panes"), `"metricsQueryType":"range"`)
		assert.Contains(t, params.Get("panes"), `"serviceMapUseNativeHistograms":false`)
		assert.Contains(t, params.Get("panes"), `"query":"287e20fb791cf30"`)
		assert.Contains(t, params.Get("panes"), `"from":"2026-04-24T10:00:00Z"`)
		assert.Contains(t, params.Get("panes"), `"to":"2026-04-24T11:00:00Z"`)
		assert.Contains(t, params.Get("panes"), `"compact":false`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, tempo.TraceExploreURL("", dsquery.ExploreQuery{DatasourceUID: "tempo-uid"}, "abc"))
		assert.Empty(t, tempo.TraceExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{}, "abc"))
		assert.Empty(t, tempo.TraceExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{DatasourceUID: "tempo-uid"}, ""))
	})
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
