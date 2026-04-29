package prometheus

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// QueryExploreURL builds a Grafana Explore URL for a Prometheus query.
func QueryExploreURL(host string, query dsquery.ExploreQuery) string {
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || strings.TrimSpace(query.Expr) == "" {
		return ""
	}

	from, to := dsquery.ExploreRange(query.From, query.To, query.Instant)

	q := map[string]any{
		"refId":      "A",
		"expr":       query.Expr,
		"editorMode": "code",
		"instant":    query.Instant,
		"range":      !query.Instant,
		"datasource": dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
	}
	if query.Step > 0 {
		q["intervalMs"] = query.Step.Milliseconds()
	}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, []any{q}, from, to, nil), nil)
}
