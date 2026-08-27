package pinot_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/pinot"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	querypinot "github.com/grafana/gcx/internal/query/pinot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryExploreURL(t *testing.T) {
	t.Run("builds explore link", func(t *testing.T) {
		got := pinot.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "pinot-uid",
			DatasourceType: querypinot.DatasourceType,
			Expr:           "SELECT count(*) FROM faro_pinot_events_v2",
			OrgID:          1,
		}, "faro_pinot_events_v2")

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "1", params.Get("orgId"))
		assert.Contains(t, params.Get("panes"), `"datasource":"pinot-uid"`)
		assert.Contains(t, params.Get("panes"), `"type":"startree-pinot-datasource"`)
		assert.Contains(t, params.Get("panes"), `"queryType":"PinotQL"`)
		assert.Contains(t, params.Get("panes"), `"editorMode":"Code"`)
		assert.Contains(t, params.Get("panes"), `"displayType":"TABLE"`)
		assert.Contains(t, params.Get("panes"), `"tableName":"faro_pinot_events_v2"`)
		assert.Contains(t, params.Get("panes"), `"pinotQlCode":"SELECT count(*) FROM faro_pinot_events_v2"`)
		assert.Contains(t, params.Get("panes"), `"from":"now-1h"`)
		assert.Contains(t, params.Get("panes"), `"to":"now"`)
	})

	t.Run("includes explicit time range", func(t *testing.T) {
		got := pinot.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "pinot-uid",
			DatasourceType: querypinot.DatasourceType,
			Expr:           "SELECT 1",
			From:           "2026-05-10T10:00:00Z",
			To:             "2026-05-10T11:00:00Z",
		}, "faro_pinot_events_v2")

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Contains(t, params.Get("panes"), `"from":"2026-05-10T10:00:00Z"`)
		assert.Contains(t, params.Get("panes"), `"to":"2026-05-10T11:00:00Z"`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, pinot.QueryExploreURL("", dsquery.ExploreQuery{DatasourceUID: "pinot-uid", Expr: "SELECT 1"}, "t"))
		assert.Empty(t, pinot.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{Expr: "SELECT 1"}, "t"))
		assert.Empty(t, pinot.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{DatasourceUID: "pinot-uid"}, "t"))
	})
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
