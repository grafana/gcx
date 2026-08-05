package cloudmonitoring

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	gcmclient "github.com/grafana/gcx/internal/query/cloudmonitoring"
)

// QueryExploreURL builds a Grafana Explore URL for a Google Cloud Monitoring
// metrics query.
//
// A Cloud Monitoring query is structured (project, metric type, reducer,
// aligner, filters), not an expression string, so the pane query comes from
// gcmclient.BuildQueryModel — the same builder the client uses for its request
// body.
//
// base supplies the datasource UID, the time range, and the org ID.
// base.Expr and base.DatasourceType are unused: req carries the query, and the
// builder stamps the plugin type. The Start/End fields on req are also ignored,
// because the Explore pane range carries the time span.
func QueryExploreURL(host string, base dsquery.ExploreQuery, req gcmclient.QueryRequest) string {
	if strings.TrimSpace(host) == "" || base.DatasourceUID == "" ||
		strings.TrimSpace(req.Project) == "" || strings.TrimSpace(req.MetricType) == "" {
		return ""
	}

	from, to := dsquery.ExploreRange(base.From, base.To, false)

	return dsquery.BuildExploreURL(
		host,
		base.OrgID,
		dsquery.SinglePane(base.DatasourceUID, []any{gcmclient.BuildQueryModel(base.DatasourceUID, req)}, from, to, nil),
		nil,
	)
}
