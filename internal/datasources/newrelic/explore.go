package newrelic

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/newrelic"
)

// QueryExploreURL carries the executed plugin overrides
// into Explore.
func QueryExploreURL(host string, query dsquery.ExploreQuery, req newrelic.QueryRequest) string {
	if strings.TrimSpace(query.DatasourceUID) == "" || strings.TrimSpace(req.QueryText) == "" {
		return ""
	}
	from, to := dsquery.ExploreRange(query.From, query.To, false)
	return dsquery.BuildExploreURL(host, query.OrgID, dsquery.SinglePane(query.DatasourceUID, []any{req.Model(query.DatasourceUID)}, from, to, nil), nil)
}
