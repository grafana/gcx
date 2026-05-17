package cloudwatch_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/cloudwatch"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}

func baseQuery(uid string) dsquery.ExploreQuery {
	return dsquery.ExploreQuery{
		DatasourceUID:  uid,
		DatasourceType: "cloudwatch",
	}
}

func baseCloudWatchQuery() cloudwatch.CloudWatchQuery {
	return cloudwatch.CloudWatchQuery{
		Namespace:  "AWS/EC2",
		MetricName: "CPUUtilization",
		Region:     "us-east-1",
		Statistic:  "Average",
		Period:     300,
	}
}

func TestQueryExploreURL_StructuredPayload(t *testing.T) {
	got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), baseCloudWatchQuery())

	require.NotEmpty(t, got)
	params := mustParseURL(t, got).Query()
	panes := params.Get("panes")

	assert.Contains(t, panes, `"namespace":"AWS/EC2"`)
	assert.Contains(t, panes, `"metricName":"CPUUtilization"`)
	assert.Contains(t, panes, `"region":"us-east-1"`)
	assert.Contains(t, panes, `"statistic":"Average"`)
	assert.Contains(t, panes, `"uid":"cw-uid"`)
}

func TestQueryExploreURL_MatchExactFalseWhenNoDimensions(t *testing.T) {
	q := baseCloudWatchQuery()
	q.Dimensions = nil // no dimensions

	got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), q)
	require.NotEmpty(t, got)
	params := mustParseURL(t, got).Query()
	assert.Contains(t, params.Get("panes"), `"matchExact":false`)
}

func TestQueryExploreURL_MatchExactTrueWhenDimensionsSet(t *testing.T) {
	q := baseCloudWatchQuery()
	q.MatchExact = true
	q.Dimensions = map[string]string{"InstanceId": "i-abc"}

	got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), q)
	require.NotEmpty(t, got)
	params := mustParseURL(t, got).Query()
	assert.Contains(t, params.Get("panes"), `"matchExact":true`)
	assert.Contains(t, params.Get("panes"), `"InstanceId"`)
}

func TestQueryExploreURL_ExplicitTimeRange(t *testing.T) {
	base := baseQuery("cw-uid")
	base.From = "2026-05-17T08:00:00Z"
	base.To = "2026-05-17T09:00:00Z"

	got := cloudwatch.QueryExploreURL("https://test.grafana.net", base, baseCloudWatchQuery())
	require.NotEmpty(t, got)
	params := mustParseURL(t, got).Query()
	assert.Contains(t, params.Get("panes"), `"from":"2026-05-17T08:00:00Z"`)
	assert.Contains(t, params.Get("panes"), `"to":"2026-05-17T09:00:00Z"`)
}

func TestQueryExploreURL_DefaultTimeRange(t *testing.T) {
	got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), baseCloudWatchQuery())
	require.NotEmpty(t, got)
	params := mustParseURL(t, got).Query()
	// Default: "now-1h" / "now" when no From/To set.
	assert.Contains(t, params.Get("panes"), `"from":"now-1h"`)
	assert.Contains(t, params.Get("panes"), `"to":"now"`)
}

func TestQueryExploreURL_AccountID(t *testing.T) {
	q := baseCloudWatchQuery()
	q.AccountID = "123456789"

	got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), q)
	require.NotEmpty(t, got)
	params := mustParseURL(t, got).Query()
	assert.Contains(t, params.Get("panes"), `"accountId":"123456789"`)
}

func TestQueryExploreURL_ReturnsEmptyOnMissingRequired(t *testing.T) {
	t.Run("empty host", func(t *testing.T) {
		assert.Empty(t, cloudwatch.QueryExploreURL("", baseQuery("uid"), baseCloudWatchQuery()))
	})
	t.Run("empty UID", func(t *testing.T) {
		assert.Empty(t, cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery(""), baseCloudWatchQuery()))
	})
	t.Run("empty namespace", func(t *testing.T) {
		q := baseCloudWatchQuery()
		q.Namespace = ""
		assert.Empty(t, cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("uid"), q))
	})
	t.Run("empty metric", func(t *testing.T) {
		q := baseCloudWatchQuery()
		q.MetricName = ""
		assert.Empty(t, cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("uid"), q))
	})
	t.Run("empty region", func(t *testing.T) {
		q := baseCloudWatchQuery()
		q.Region = ""
		assert.Empty(t, cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("uid"), q))
	})
}
