package infinity

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// QueryExploreURL builds a Grafana Explore URL for an Infinity query. The
// query model mirrors the one sent by the query client; Expr is the optional
// root selector and may be empty.
func QueryExploreURL(host string, query dsquery.ExploreQuery) string {
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" {
		return ""
	}

	from, to := dsquery.ExploreRange(query.From, query.To, false)

	q := map[string]any{
		"refId":            "A",
		"datasource":       dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
		"type":             "json",
		"source":           "url",
		"format":           "table",
		"url":              "",
		"parser":           "backend",
		"root_selector":    query.Expr,
		"columns":          []any{},
		"filters":          []any{},
		"computed_columns": []any{},
		"filterExpression": "",
		"uql":              "",
		"groq":             "",
	}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, []any{q}, from, to, nil), nil)
}
