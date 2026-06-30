package athena_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/athena"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryExploreURL(t *testing.T) {
	t.Run("builds explore link", func(t *testing.T) {
		got := athena.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "athena-uid",
			DatasourceType: "grafana-athena-datasource",
			Expr:           "SELECT count(*) FROM events",
			OrgID:          1,
		})

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "1", params.Get("orgId"))
		assert.Contains(t, params.Get("panes"), `"datasource":"athena-uid"`)
		assert.Contains(t, params.Get("panes"), `"rawSQL":"SELECT count(*) FROM events"`)
		assert.Contains(t, params.Get("panes"), `"editorMode":"code"`)
		assert.Contains(t, params.Get("panes"), `"from":"now-1h"`)
		assert.Contains(t, params.Get("panes"), `"to":"now"`)
	})

	t.Run("includes explicit time range", func(t *testing.T) {
		got := athena.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "athena-uid",
			DatasourceType: "grafana-athena-datasource",
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
		assert.Empty(t, athena.QueryExploreURL("", dsquery.ExploreQuery{DatasourceUID: "athena-uid", Expr: "SELECT 1"}))
		assert.Empty(t, athena.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{Expr: "SELECT 1"}))
		assert.Empty(t, athena.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{DatasourceUID: "athena-uid"}))
	})
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
