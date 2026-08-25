package azuremonitor

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	azclient "github.com/grafana/gcx/internal/query/azuremonitor"
)

// The Azure Monitor plugin has three distinct query models, each with its own
// queryType. Explore rejects or silently misreads a query object that does not
// match the plugin's model, so there is one URL builder per model, and each one
// reuses the same query-map builder the client uses to POST the query.

// QueryExploreURL builds a Grafana Explore URL for an Azure Monitor metrics
// query. The query object mirrors azclient.Client.Query, because both call
// azclient.MetricsQueryModel.
func QueryExploreURL(host string, link dsquery.ExploreQuery, req azclient.QueryRequest) string {
	if strings.TrimSpace(req.MetricName) == "" || strings.TrimSpace(req.Subscription) == "" {
		return ""
	}
	return buildExploreURL(host, link, azclient.MetricsQueryModel(link.DatasourceUID, req))
}

// LogsExploreURL builds a Grafana Explore URL for an Azure Log Analytics KQL
// query. The query object mirrors azclient.Client.LogsQuery, because both call
// azclient.LogsQueryModel.
func LogsExploreURL(host string, link dsquery.ExploreQuery, req azclient.LogsQueryRequest) string {
	if strings.TrimSpace(req.Query) == "" || strings.TrimSpace(req.Workspace) == "" {
		return ""
	}
	return buildExploreURL(host, link, azclient.LogsQueryModel(link.DatasourceUID, req))
}

// ResourceGraphExploreURL builds a Grafana Explore URL for an Azure Resource
// Graph KQL query. The query object mirrors
// azclient.Client.ResourceGraphQuery, because both call
// azclient.ResourceGraphQueryModel.
func ResourceGraphExploreURL(host string, link dsquery.ExploreQuery, req azclient.ResourceGraphRequest) string {
	if strings.TrimSpace(req.Query) == "" || len(req.Subscriptions) == 0 {
		return ""
	}
	return buildExploreURL(host, link, azclient.ResourceGraphQueryModel(link.DatasourceUID, req))
}

// buildExploreURL wraps one already-built Azure Monitor query model in a single
// Explore pane. The pane range carries the time span, so the query model keeps
// no from/to of its own.
func buildExploreURL(host string, link dsquery.ExploreQuery, query map[string]any) string {
	if strings.TrimSpace(host) == "" || link.DatasourceUID == "" {
		return ""
	}

	// The query model names the plugin ID directly; prefer the datasource type
	// that resolution reported when it is available.
	if link.DatasourceType != "" {
		query["datasource"] = dsquery.ExploreDatasource(link.DatasourceType, link.DatasourceUID)
	}

	from, to := dsquery.ExploreRange(link.From, link.To, false)

	return dsquery.BuildExploreURL(host, link.OrgID, dsquery.SinglePane(link.DatasourceUID, []any{query}, from, to, nil), nil)
}
