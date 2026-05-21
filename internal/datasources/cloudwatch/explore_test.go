package cloudwatch_test

import (
	"net/url"
	"testing"

	"github.com/grafana/gcx/internal/datasources/cloudwatch"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	cwclient "github.com/grafana/gcx/internal/query/cloudwatch"
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

func baseCloudWatchQuery() cwclient.QueryRequest {
	return cwclient.QueryRequest{
		Namespace:  "AWS/EC2",
		MetricName: "CPUUtilization",
		Region:     "us-east-1",
		Statistic:  "Average",
		Period:     "300",
	}
}

func TestQueryExploreURL(t *testing.T) {
	t.Run("structured payload", func(t *testing.T) {
		got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), baseCloudWatchQuery())

		require.NotEmpty(t, got)
		panes := mustParseURL(t, got).Query().Get("panes")

		assert.Contains(t, panes, `"namespace":"AWS/EC2"`)
		assert.Contains(t, panes, `"metricName":"CPUUtilization"`)
		assert.Contains(t, panes, `"region":"us-east-1"`)
		assert.Contains(t, panes, `"statistic":"Average"`)
		assert.Contains(t, panes, `"uid":"cw-uid"`)
	})

	t.Run("period encoded as JSON string", func(t *testing.T) {
		// Cloudwatch plugin's dataquery schema declares period as a string.
		q := baseCloudWatchQuery()
		q.Period = "300"

		got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), q)
		require.NotEmpty(t, got)
		panes := mustParseURL(t, got).Query().Get("panes")
		assert.Contains(t, panes, `"period":"300"`)
		assert.NotContains(t, panes, `"period":300`)
	})

	t.Run("period 'auto' propagates verbatim", func(t *testing.T) {
		q := baseCloudWatchQuery()
		q.Period = "auto"

		got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), q)
		require.NotEmpty(t, got)
		assert.Contains(t, mustParseURL(t, got).Query().Get("panes"), `"period":"auto"`)
	})

	t.Run("matchExact false when no dimensions", func(t *testing.T) {
		q := baseCloudWatchQuery()
		q.Dimensions = nil

		got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), q)
		require.NotEmpty(t, got)
		assert.Contains(t, mustParseURL(t, got).Query().Get("panes"), `"matchExact":false`)
	})

	t.Run("matchExact true when dimensions set", func(t *testing.T) {
		q := baseCloudWatchQuery()
		q.MatchExact = true
		q.Dimensions = map[string][]string{"InstanceId": {"i-abc"}}

		got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), q)
		require.NotEmpty(t, got)
		panes := mustParseURL(t, got).Query().Get("panes")
		assert.Contains(t, panes, `"matchExact":true`)
		assert.Contains(t, panes, `"InstanceId":["i-abc"]`)
	})

	t.Run("explicit time range", func(t *testing.T) {
		base := baseQuery("cw-uid")
		base.From = "2026-05-17T08:00:00Z"
		base.To = "2026-05-17T09:00:00Z"

		got := cloudwatch.QueryExploreURL("https://test.grafana.net", base, baseCloudWatchQuery())
		require.NotEmpty(t, got)
		panes := mustParseURL(t, got).Query().Get("panes")
		assert.Contains(t, panes, `"from":"2026-05-17T08:00:00Z"`)
		assert.Contains(t, panes, `"to":"2026-05-17T09:00:00Z"`)
	})

	t.Run("default time range is now-1h/now", func(t *testing.T) {
		got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), baseCloudWatchQuery())
		require.NotEmpty(t, got)
		panes := mustParseURL(t, got).Query().Get("panes")
		assert.Contains(t, panes, `"from":"now-1h"`)
		assert.Contains(t, panes, `"to":"now"`)
	})

	t.Run("accountId propagates when set", func(t *testing.T) {
		q := baseCloudWatchQuery()
		q.AccountID = "123456789"

		got := cloudwatch.QueryExploreURL("https://test.grafana.net", baseQuery("cw-uid"), q)
		require.NotEmpty(t, got)
		assert.Contains(t, mustParseURL(t, got).Query().Get("panes"), `"accountId":"123456789"`)
	})

	t.Run("returns empty on missing required fields", func(t *testing.T) {
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
	})
}
