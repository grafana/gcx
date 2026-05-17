package cloudwatch

import (
	"strings"

	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
)

// CloudWatchQuery holds the structured CloudWatch query fields for Explore URL building.
type CloudWatchQuery struct {
	Namespace  string
	MetricName string
	Region     string
	Statistic  string
	AccountID  string
	Dimensions map[string]string
	MatchExact bool
	Period     int
}

// QueryExploreURL builds a Grafana Explore URL for a structured CloudWatch metric query.
// Returns "" when any required parameter (host, UID, namespace, metric, region) is missing.
func QueryExploreURL(host string, base dsquery.ExploreQuery, q CloudWatchQuery) string {
	if strings.TrimSpace(host) == "" || base.DatasourceUID == "" ||
		q.Namespace == "" || q.MetricName == "" || q.Region == "" {
		return ""
	}

	from, to := dsquery.ExploreRange(base.From, base.To, false)

	query := map[string]any{
		"refId":      "A",
		"datasource": dsquery.ExploreDatasource("cloudwatch", base.DatasourceUID),
		"queryType":  "timeSeriesQuery",
		"namespace":  q.Namespace,
		"metricName": q.MetricName,
		"region":     q.Region,
		"statistic":  q.Statistic,
		"matchExact": q.MatchExact,
		"dimensions": cwclient.FormatDimensionsForExplore(q.Dimensions),
	}
	if q.Period > 0 {
		query["period"] = q.Period
	}
	if q.AccountID != "" {
		query["accountId"] = q.AccountID
	}

	return dsquery.BuildExploreURL(
		host,
		base.OrgID,
		dsquery.SinglePane(base.DatasourceUID, []any{query}, from, to, nil),
		nil,
	)
}
