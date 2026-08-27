package bigquery_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/bigquery"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryExploreURL(t *testing.T) {
	t.Run("builds explore link", func(t *testing.T) {
		got := bigquery.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "bq-uid",
			DatasourceType: "grafana-bigquery-datasource",
			Expr:           "SELECT count(*) FROM `ds.events`",
			OrgID:          1,
		})

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "1", params.Get("orgId"))
		assert.Contains(t, params.Get("panes"), `"datasource":"bq-uid"`)
		assert.Contains(t, params.Get("panes"), `"editorMode":"code"`)
		assert.Contains(t, params.Get("panes"), `"from":"now-1h"`)
		assert.Contains(t, params.Get("panes"), `"to":"now"`)
		// Must match query/bigquery.QueryFormatTable, the value client.go
		// actually sends — a literal here would silently drift from it.
		assert.Contains(t, params.Get("panes"), `"format":1`)
	})

	t.Run("includes explicit time range", func(t *testing.T) {
		got := bigquery.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "bq-uid",
			DatasourceType: "grafana-bigquery-datasource",
			Expr:           "SELECT 1",
			From:           "2026-05-10T10:00:00Z",
			To:             "2026-05-10T11:00:00Z",
		})

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Contains(t, params.Get("panes"), `"from":"2026-05-10T10:00:00Z"`)
		assert.Contains(t, params.Get("panes"), `"to":"2026-05-10T11:00:00Z"`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, bigquery.QueryExploreURL("", dsquery.ExploreQuery{DatasourceUID: "bq-uid", Expr: "SELECT 1"}))
		assert.Empty(t, bigquery.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{Expr: "SELECT 1"}))
		assert.Empty(t, bigquery.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{DatasourceUID: "bq-uid"}))
	})
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
