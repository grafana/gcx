package influxdb_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/influxdb"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	qinflux "github.com/grafana/gcx/internal/query/influxdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func panesOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "/explore", u.Path)
	assert.Equal(t, "1", u.Query().Get("schemaVersion"))
	return u.Query().Get("panes")
}

func TestQueryExploreURL(t *testing.T) {
	base := dsquery.ExploreQuery{
		DatasourceUID:  "influx-uid",
		DatasourceType: "influxdb",
		Expr:           `SELECT mean("value") FROM "cpu"`,
		OrgID:          1,
	}

	t.Run("influxql builds raw table query", func(t *testing.T) {
		got := influxdb.QueryExploreURL("https://mystack.grafana.net", base, qinflux.ModeInfluxQL)
		require.NotEmpty(t, got)
		panes := panesOf(t, got)
		assert.Contains(t, panes, `"datasource":"influx-uid"`)
		assert.Contains(t, panes, `"datasource":{"type":"influxdb","uid":"influx-uid"}`)
		assert.Contains(t, panes, `"query":"SELECT mean(\"value\") FROM \"cpu\""`)
		assert.Contains(t, panes, `"rawQuery":true`)
		assert.Contains(t, panes, `"resultFormat":"table"`)
		assert.Contains(t, panes, `"from":"now-1h"`)
		assert.Contains(t, panes, `"to":"now"`)
	})

	t.Run("sql mode builds raw table query", func(t *testing.T) {
		panes := panesOf(t, influxdb.QueryExploreURL("https://mystack.grafana.net", base, qinflux.ModeSQL))
		assert.Contains(t, panes, `"rawQuery":true`)
	})

	t.Run("flux carries only query text", func(t *testing.T) {
		q := base
		q.Expr = `from(bucket: "b")`
		panes := panesOf(t, influxdb.QueryExploreURL("https://mystack.grafana.net", q, qinflux.ModeFlux))
		assert.Contains(t, panes, `"query":"from(bucket: \"b\")"`)
		assert.NotContains(t, panes, "rawQuery")
		assert.NotContains(t, panes, "resultFormat")
	})

	t.Run("includes explicit time range", func(t *testing.T) {
		q := base
		q.From = "2026-05-10T10:00:00Z"
		q.To = "2026-05-10T11:00:00Z"
		panes := panesOf(t, influxdb.QueryExploreURL("https://mystack.grafana.net", q, qinflux.ModeInfluxQL))
		assert.Contains(t, panes, `"from":"2026-05-10T10:00:00Z"`)
		assert.Contains(t, panes, `"to":"2026-05-10T11:00:00Z"`)
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		assert.Empty(t, influxdb.QueryExploreURL("", base, qinflux.ModeInfluxQL))
		noUID := base
		noUID.DatasourceUID = ""
		assert.Empty(t, influxdb.QueryExploreURL("https://mystack.grafana.net", noUID, qinflux.ModeInfluxQL))
		noExpr := base
		noExpr.Expr = ""
		assert.Empty(t, influxdb.QueryExploreURL("https://mystack.grafana.net", noExpr, qinflux.ModeInfluxQL))
	})
}
