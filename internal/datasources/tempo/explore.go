package tempo

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// SearchExploreURL builds a Grafana Explore URL for a Tempo TraceQL search query.
func SearchExploreURL(host string, query dsquery.ExploreQuery, limit int) string {
	return buildTraceQLExploreURL(host, query, limit, "range")
}

// MetricsExploreURL builds a Grafana Explore URL for a Tempo TraceQL metrics query.
func MetricsExploreURL(host string, query dsquery.ExploreQuery, limit int) string {
	metricsQueryType := "range"
	if query.Instant {
		metricsQueryType = "instant"
	}
	return buildTraceQLExploreURL(host, query, limit, metricsQueryType)
}

func buildTraceQLExploreURL(host string, query dsquery.ExploreQuery, limit int, metricsQueryType string) string {
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || strings.TrimSpace(query.Expr) == "" {
		return ""
	}
	if limit == 0 {
		return ""
	}
	if limit < 0 {
		limit = 20
	}

	from, to := dsquery.ShortExploreRange(query.From, query.To)

	queries := []any{map[string]any{
		"refId":                         "A",
		"datasource":                    dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
		"queryType":                     "traceql",
		"limit":                         limit,
		"tableType":                     "traces",
		"metricsQueryType":              metricsQueryType,
		"serviceMapUseNativeHistograms": false,
		"query":                         query.Expr,
	}}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, queries, from, to, map[string]any{
		"compact": false,
	}), nil)
}

// TraceExploreURL builds a Grafana Explore URL for a Tempo trace ID lookup.
func TraceExploreURL(host string, query dsquery.ExploreQuery, traceID string) string {
	traceID = strings.TrimSpace(traceID)
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || traceID == "" {
		return ""
	}

	from, to := dsquery.ShortExploreRange(query.From, query.To)

	queries := []any{map[string]any{
		"refId":                         "A",
		"datasource":                    dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
		"queryType":                     "traceql",
		"limit":                         20,
		"tableType":                     "traces",
		"metricsQueryType":              "range",
		"serviceMapUseNativeHistograms": false,
		"query":                         traceID,
	}}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, queries, from, to, map[string]any{
		"compact": false,
	}), map[string]string{"traceId": traceID})
}
