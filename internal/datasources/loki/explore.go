package loki

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// LogsExploreURL builds a Grafana Explore URL for a Loki log-lines query.
func LogsExploreURL(host string, query dsquery.ExploreQuery) string {
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || strings.TrimSpace(query.Expr) == "" {
		return ""
	}

	from, to := dsquery.ShortExploreRange(query.From, query.To)

	queries := []any{map[string]any{
		"refId":      "A",
		"expr":       query.Expr,
		"queryType":  "range",
		"editorMode": "code",
		"direction":  "backward",
		"datasource": dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
	}}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, queries, from, to, map[string]any{
		"panelsState": map[string]any{
			"logs": map[string]any{
				"sortOrder": "Descending",
			},
		},
		"compact": false,
	}), nil)
}

// MetricsExploreURL builds a Grafana Explore URL for a Loki metric LogQL query.
func MetricsExploreURL(host string, query dsquery.ExploreQuery) string {
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || strings.TrimSpace(query.Expr) == "" {
		return ""
	}

	from, to := dsquery.ShortExploreRange(query.From, query.To)

	queries := []any{map[string]any{
		"refId":      "A",
		"expr":       query.Expr,
		"queryType":  "range",
		"instant":    query.Instant,
		"range":      !query.Instant,
		"editorMode": "code",
		"direction":  "backward",
		"datasource": dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
	}}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, queries, from, to, map[string]any{
		"compact": false,
	}), nil)
}
