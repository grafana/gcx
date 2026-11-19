package mysql

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// QueryExploreURL builds a Grafana Explore URL for a MySQL SQL query.
func QueryExploreURL(host string, query dsquery.ExploreQuery) string {
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || strings.TrimSpace(query.Expr) == "" {
		return ""
	}

	from, to := dsquery.ExploreRange(query.From, query.To, false)

	q := map[string]any{
		"refId":      "A",
		"rawSql":     query.Expr,
		"rawQuery":   true,
		"editorMode": "code",
		"format":     "table",
		"datasource": dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
	}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, []any{q}, from, to, nil), nil)
}
