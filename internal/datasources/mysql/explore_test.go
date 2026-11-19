package mysql_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/mysql"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryExploreURL(t *testing.T) {
	t.Run("builds explore link", func(t *testing.T) {
		got := mysql.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "mysql-uid",
			DatasourceType: "mysql",
			Expr:           "SELECT count(*) FROM orders",
			OrgID:          1,
		})

		require.NotEmpty(t, got)
		u, err := url.Parse(got)
		require.NoError(t, err)
		assert.Equal(t, "/explore", u.Path)
		params := u.Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "1", params.Get("orgId"))
		panes := params.Get("panes")
		assert.Contains(t, panes, `"datasource":"mysql-uid"`)
		assert.Contains(t, panes, `"datasource":{"type":"mysql","uid":"mysql-uid"}`)
		assert.Contains(t, panes, `"rawSql":"SELECT count(*) FROM orders"`)
		assert.Contains(t, panes, `"rawQuery":true`)
		assert.Contains(t, panes, `"editorMode":"code"`)
		assert.Contains(t, panes, `"format":"table"`)
		assert.Contains(t, panes, `"from":"now-1h"`)
		assert.Contains(t, panes, `"to":"now"`)
	})

	t.Run("includes explicit time range", func(t *testing.T) {
		got := mysql.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "mysql-uid",
			DatasourceType: "mysql",
			Expr:           "SELECT 1",
			From:           "2026-05-10T10:00:00Z",
			To:             "2026-05-10T11:00:00Z",
		})

		require.NotEmpty(t, got)
		u, err := url.Parse(got)
		require.NoError(t, err)
		panes := u.Query().Get("panes")
		assert.Contains(t, panes, `"from":"2026-05-10T10:00:00Z"`)
		assert.Contains(t, panes, `"to":"2026-05-10T11:00:00Z"`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, mysql.QueryExploreURL("", dsquery.ExploreQuery{DatasourceUID: "u", Expr: "SELECT 1"}))
		assert.Empty(t, mysql.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{Expr: "SELECT 1"}))
		assert.Empty(t, mysql.QueryExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{DatasourceUID: "u"}))
	})
}
