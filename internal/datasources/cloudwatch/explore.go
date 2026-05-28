package cloudwatch

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
)

// QueryExploreURL builds a Grafana Explore URL for a CloudWatch metric query.
// Time fields on req are ignored; the Explore range comes from base.From/base.To.
func QueryExploreURL(host string, base dsquery.ExploreQuery, req cwclient.QueryRequest) string {
	if strings.TrimSpace(host) == "" || base.DatasourceUID == "" ||
		req.Namespace == "" || req.MetricName == "" || req.Region == "" {
		return ""
	}

	from, to := dsquery.ExploreRange(base.From, base.To, false)

	dimensions := req.Dimensions
	if dimensions == nil {
		dimensions = map[string][]string{}
	}

	query := map[string]any{
		"refId":      "A",
		"datasource": dsquery.ExploreDatasource("cloudwatch", base.DatasourceUID),
		"queryType":  "timeSeriesQuery",
		"namespace":  req.Namespace,
		"metricName": req.MetricName,
		"region":     req.Region,
		"statistic":  req.Statistic,
		"matchExact": req.MatchExact,
		"dimensions": dimensions,
	}
	if req.Period != "" {
		query["period"] = req.Period
	}
	if req.AccountID != "" {
		query["accountId"] = req.AccountID
	}

	return dsquery.BuildExploreURL(
		host,
		base.OrgID,
		dsquery.SinglePane(base.DatasourceUID, []any{query}, from, to, nil),
		nil,
	)
}
