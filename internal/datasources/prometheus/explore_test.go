package prometheus_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/datasources/prometheus"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExploreURL(t *testing.T) {
	t.Run("builds instant explore link", func(t *testing.T) {
		got := prometheus.ExploreURL("https://mystack.grafana.net/", dsquery.ExploreQuery{
			DatasourceUID:  "prom-uid",
			DatasourceType: "prometheus",
			Expr:           `up{job="grafana/server"}`,
			Instant:        true,
			OrgID:          7,
		})

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Equal(t, "1", params.Get("schemaVersion"))
		assert.Equal(t, "7", params.Get("orgId"))
		assert.Contains(t, params.Get("panes"), `"expr":"up{job=\"grafana/server\"}"`)
		assert.Contains(t, params.Get("panes"), `"uid":"prom-uid"`)
		assert.Contains(t, params.Get("panes"), `"instant":true`)
		assert.Contains(t, params.Get("panes"), `"range":false`)
		assert.Contains(t, params.Get("panes"), `"from":"now-1m"`)
		assert.Contains(t, params.Get("panes"), `"to":"now"`)
	})

	t.Run("builds range explore link with explicit time bounds", func(t *testing.T) {
		got := prometheus.ExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{
			DatasourceUID:  "prom-uid",
			DatasourceType: "prometheus",
			Expr:           "rate(http_requests_total[5m])",
			From:           "2026-04-24T10:00:00Z",
			To:             "2026-04-24T11:00:00Z",
			Step:           time.Minute,
		})

		require.NotEmpty(t, got)
		params := mustParseURL(t, got).Query()
		assert.Contains(t, params.Get("panes"), `"expr":"rate(http_requests_total[5m])"`)
		assert.Contains(t, params.Get("panes"), `"instant":false`)
		assert.Contains(t, params.Get("panes"), `"range":true`)
		assert.Contains(t, params.Get("panes"), `"from":"2026-04-24T10:00:00Z"`)
		assert.Contains(t, params.Get("panes"), `"to":"2026-04-24T11:00:00Z"`)
		assert.Contains(t, params.Get("panes"), `"intervalMs":60000`)
	})

	t.Run("returns empty for missing host or required query fields", func(t *testing.T) {
		assert.Empty(t, prometheus.ExploreURL("", dsquery.ExploreQuery{DatasourceUID: "prom-uid", Expr: "up"}))
		assert.Empty(t, prometheus.ExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{Expr: "up"}))
		assert.Empty(t, prometheus.ExploreURL("https://mystack.grafana.net", dsquery.ExploreQuery{DatasourceUID: "prom-uid"}))
	})
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return u
}
