package pyroscope

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
)

// QueryExploreURL builds a Grafana Explore URL for a Pyroscope profile query,
// using the query object shape confirmed from
// github.com/grafana/grafana-pyroscope-datasource's CUE-generated
// src/dataquery.ts (GrafanaPyroscopeDataQuery). queryType is always "profile"
// to match pyroscope query's flamegraph output (as opposed to "metrics" or
// "both").
//
// traceIDs has no field anywhere in that schema, so --trace-id has no
// Explore-UI representation and is not part of this query object — the
// Explore link is simply inaccurate for that one flag, same as it is today
// for every other gcx datasource lacking an equivalent field.
func QueryExploreURL(host string, query dsquery.ExploreQuery, profileType string, spanIDs, profileIDs, stacktraceSelector []string, maxNodes int64) string {
	if strings.TrimSpace(host) == "" || query.DatasourceUID == "" || strings.TrimSpace(query.Expr) == "" || profileType == "" {
		return ""
	}

	from, to := dsquery.ShortExploreRange(query.From, query.To)

	q := map[string]any{
		"refId":         "A",
		"datasource":    dsquery.ExploreDatasource(query.DatasourceType, query.DatasourceUID),
		"queryType":     "profile",
		"labelSelector": query.Expr,
		"profileTypeId": profileType,
		"groupBy":       []string{},
	}
	if len(spanIDs) > 0 {
		q["spanSelector"] = spanIDs
	}
	if len(profileIDs) > 0 {
		q["profileIdSelector"] = profileIDs
	}
	if len(stacktraceSelector) > 0 {
		q["stackTraceSelector"] = stacktraceSelector
	}
	if maxNodes > 0 {
		q["maxNodes"] = maxNodes
	}

	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, []any{q}, from, to, nil), nil)
}
