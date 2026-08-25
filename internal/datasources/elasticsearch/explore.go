package elasticsearch

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/elasticsearch"
)

// Elasticsearch has three distinct query models — a raw_data document search,
// a logs search, and a metric aggregation over a date histogram — so each one
// gets its own Explore URL builder. Every builder takes the query model from
// the client package, which the client itself also uses. The two shapes
// therefore cannot drift apart.
//
// An empty Lucene expression is valid: it matches all documents in the range.
// The builders therefore guard on the host and the datasource UID only.

// QueryExploreURL builds a Grafana Explore URL for an Elasticsearch document
// search. It mirrors the body of elasticsearch.Client.Search.
// Time fields on req are ignored; the Explore range comes from base.From/base.To.
func QueryExploreURL(host string, base dsquery.ExploreQuery, req elasticsearch.SearchRequest) string {
	return exploreURL(host, base, elasticsearch.SearchQueryModel(base.DatasourceUID, req), nil)
}

// LogsExploreURL builds a Grafana Explore URL for an Elasticsearch log search.
// It mirrors the body of elasticsearch.Client.Logs.
// Time fields on req are ignored; the Explore range comes from base.From/base.To.
func LogsExploreURL(host string, base dsquery.ExploreQuery, req elasticsearch.SearchRequest) string {
	return exploreURL(host, base, elasticsearch.LogsQueryModel(base.DatasourceUID, req),
		map[string]any{
			"panelsState": map[string]any{
				"logs": map[string]any{
					"sortOrder": "Descending",
				},
			},
		})
}

// MetricsExploreURL builds a Grafana Explore URL for an Elasticsearch metric
// aggregation. It mirrors the body of elasticsearch.Client.Aggregations.
// Time fields on req are ignored; the Explore range comes from base.From/base.To.
func MetricsExploreURL(host string, base dsquery.ExploreQuery, req elasticsearch.AggsRequest) string {
	return exploreURL(host, base, elasticsearch.AggsQueryModel(base.DatasourceUID, req), nil)
}

// exploreURL renders a single-pane Explore URL around one plugin query model.
func exploreURL(host string, base dsquery.ExploreQuery, q map[string]any, paneExtra map[string]any) string {
	if strings.TrimSpace(host) == "" || base.DatasourceUID == "" {
		return ""
	}

	from, to := dsquery.ExploreRange(base.From, base.To, false)

	return dsquery.BuildExploreURL(
		host,
		base.OrgID,
		dsquery.SinglePane(base.DatasourceUID, []any{q}, from, to, paneExtra),
		nil,
	)
}
