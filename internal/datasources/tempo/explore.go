package tempo

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/tempo"
)

// SearchExploreURL builds a Grafana Explore URL for a Tempo TraceQL search query.
func SearchExploreURL(host string, query dsquery.ExploreQuery, limit int) string {
	return buildTraceQLExploreURL(host, query, limit, "range", "traces", 0)
}

// MetricsExploreURL builds a Grafana Explore URL for a Tempo TraceQL metrics query.
func MetricsExploreURL(host string, query dsquery.ExploreQuery, limit int) string {
	metricsQueryType := "range"
	if query.Instant {
		metricsQueryType = "instant"
	}
	return buildTraceQLExploreURL(host, query, limit, metricsQueryType, "traces", 0)
}

// ExceptionsExploreURL builds a Grafana Explore URL for the Exceptions tab span
// projection: tableType spans, with the same spans-per-span-set cap.
func ExceptionsExploreURL(host string, query dsquery.ExploreQuery, limit int) string {
	return buildTraceQLExploreURL(host, query, limit, "range", "spans", tempo.ExceptionsSpansPerTrace)
}

func buildTraceQLExploreURL(host string, query dsquery.ExploreQuery, limit int, metricsQueryType, tableType string, spss int) string {
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

	paneQuery := map[string]any{
		"refId":                         "A",
		"datasource":                    dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
		"queryType":                     "traceql",
		"limit":                         limit,
		"tableType":                     tableType,
		"metricsQueryType":              metricsQueryType,
		"serviceMapUseNativeHistograms": false,
		"query":                         query.Expr,
	}
	if spss > 0 {
		paneQuery["spss"] = spss
	}
	queries := []any{paneQuery}

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
