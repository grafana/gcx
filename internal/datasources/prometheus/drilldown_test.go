package prometheus_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/datasources/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsDrilldownURL_SimpleQuery(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)

	got, ok := prometheus.MetricsDrilldownURL("https://stack.grafana.net", "prom-uid",
		`rate(http_requests_total{job="grafana"}[5m])`, start, end)
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "https", u.Scheme)
	assert.Equal(t, "stack.grafana.net", u.Host)
	assert.Equal(t, "/a/grafana-metricsdrilldown-app/drilldown", u.Path)

	q := u.Query()
	assert.Equal(t, "http_requests_total", q.Get("metric"))
	assert.Equal(t, "prom-uid", q.Get("var-ds"))
	assert.Equal(t, "1704067200000", q.Get("from"))
	assert.Equal(t, "1704070800000", q.Get("to"))
	require.Len(t, q["var-filters"], 1)
	assert.Equal(t, "job|=|grafana", q["var-filters"][0])
}

func TestMetricsDrilldownURL_EscapesPipeInValue(t *testing.T) {
	got, ok := prometheus.MetricsDrilldownURL("https://stack.grafana.net", "prom-uid",
		`up{job="a|b"}`, time.Time{}, time.Time{})
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "job|=|a__gfp__b", u.Query()["var-filters"][0])
}

func TestMetricsDrilldownURL_FallbackCases(t *testing.T) {
	tests := map[string]struct {
		host, uid, expr string
	}{
		"unsupported multi-metric expression falls back": {"https://stack.grafana.net", "prom-uid", `sum(rate(a[5m]) + rate(b[5m]))`},
		"missing host falls back":                        {"", "prom-uid", `up{job="grafana"}`},
		"missing datasource UID falls back":              {"https://stack.grafana.net", "", `up{job="grafana"}`},
		"missing expr falls back":                        {"https://stack.grafana.net", "prom-uid", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := prometheus.MetricsDrilldownURL(tt.host, tt.uid, tt.expr, time.Time{}, time.Time{})
			assert.False(t, ok)
		})
	}
}
