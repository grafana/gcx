package influxdb

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/influxdb"
)

// QueryExploreURL builds a Grafana Explore URL for an InfluxDB query in the
// given query-language mode. The query model mirrors the one sent by the
// query client: Flux carries only the query text, InfluxQL and SQL are raw
// queries with table result format.
func QueryExploreURL(host string, query dsquery.ExploreQuery, mode influxdb.Mode) string {
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || strings.TrimSpace(query.Expr) == "" {
		return ""
	}

	from, to := dsquery.ExploreRange(query.From, query.To, false)

	q := map[string]any{
		"refId":      "A",
		"query":      query.Expr,
		"datasource": dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
	}
	if mode != influxdb.ModeFlux {
		q["rawQuery"] = true
		q["resultFormat"] = "table"
	}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, []any{q}, from, to, nil), nil)
}
