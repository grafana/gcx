package loki_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/datasources/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogsDrilldownURL_SimpleSelector(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)

	got, ok := loki.LogsDrilldownURL("https://stack.grafana.net", "loki-uid", `{app="foo"}`, start, end)
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "https", u.Scheme)
	assert.Equal(t, "stack.grafana.net", u.Host)
	assert.Equal(t, "/a/grafana-lokiexplore-app/explore/app/foo/logs", u.Path)

	q := u.Query()
	assert.Equal(t, "loki-uid", q.Get("var-ds"))
	assert.Equal(t, "1704067200000", q.Get("from"))
	assert.Equal(t, "1704070800000", q.Get("to"))
	require.Len(t, q["var-filters"], 1)
	assert.Equal(t, "app|=|__CVΩ__foo,foo", q["var-filters"][0])
}

func TestLogsDrilldownURL_ServiceNamePathSegment(t *testing.T) {
	got, ok := loki.LogsDrilldownURL("https://stack.grafana.net", "loki-uid", `{service_name="checkout"}`, time.Time{}, time.Time{})
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "/a/grafana-lokiexplore-app/explore/service/checkout/logs", u.Path)
}

func TestLogsDrilldownURL_LineFilters(t *testing.T) {
	got, ok := loki.LogsDrilldownURL("https://stack.grafana.net", "loki-uid", `{app="foo"} |= "error" != "debug"`, time.Time{}, time.Time{})
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	q := u.Query()
	require.Len(t, q["var-lineFilters"], 2)
	assert.Equal(t, "0|__gfp__=|error", q["var-lineFilters"][0])
	assert.Equal(t, "1|!=|debug", q["var-lineFilters"][1])
}

func TestLogsDrilldownURL_FallbackCases(t *testing.T) {
	tests := map[string]struct {
		host, uid, expr string
	}{
		"unsupported expression falls back":     {"https://stack.grafana.net", "loki-uid", `rate({app="foo"}[5m])`},
		"no inclusive label matcher falls back": {"https://stack.grafana.net", "loki-uid", `{app!="foo"}`},
		"missing host falls back":               {"", "loki-uid", `{app="foo"}`},
		"missing datasource UID falls back":     {"https://stack.grafana.net", "", `{app="foo"}`},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := loki.LogsDrilldownURL(tt.host, tt.uid, tt.expr, time.Time{}, time.Time{})
			assert.False(t, ok)
		})
	}
}
