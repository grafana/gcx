package pyroscope_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/datasources/pyroscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilesDrilldownURL_ServiceNameAndExtraFilters(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)

	got, ok := pyroscope.ProfilesDrilldownURL("https://stack.grafana.net", "pyro-uid",
		`{service_name="frontend", env="prod"}`, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", nil, false, start, end)
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "https", u.Scheme)
	assert.Equal(t, "stack.grafana.net", u.Host)
	assert.Equal(t, "/a/grafana-pyroscope-app/explore", u.Path)

	q := u.Query()
	assert.Equal(t, "pyro-uid", q.Get("var-dataSource"))
	assert.Equal(t, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", q.Get("var-profileMetricId"))
	assert.Equal(t, "frontend", q.Get("var-serviceName"))
	assert.Equal(t, "labels", q.Get("explorationType"))
	assert.Equal(t, "1704067200000", q.Get("from"))
	assert.Equal(t, "1704070800000", q.Get("to"))
	assert.Equal(t, "env|=|prod", q.Get("var-filters"))
}

func TestProfilesDrilldownURL_NoServiceNameFallsBackToAllExploration(t *testing.T) {
	got, ok := pyroscope.ProfilesDrilldownURL("https://stack.grafana.net", "pyro-uid",
		`{env="prod"}`, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", nil, false, time.Time{}, time.Time{})
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "all", q.Get("explorationType"))
	assert.Empty(t, q.Get("var-serviceName"))
	// var-filters is only emitted when explorationType is "labels".
	assert.Empty(t, q.Get("var-filters"))
}

func TestProfilesDrilldownURL_SpanSelector(t *testing.T) {
	got, ok := pyroscope.ProfilesDrilldownURL("https://stack.grafana.net", "pyro-uid",
		`{service_name="frontend"}`, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", []string{"aaa", "bbb"}, false, time.Time{}, time.Time{})
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "aaa,bbb", u.Query().Get("var-spanSelector"))
}

func TestProfilesDrilldownURL_FallbackCases(t *testing.T) {
	tests := map[string]struct {
		host, uid, selector, profileType string
		hasUnsupportedDrillDown          bool
	}{
		"trace-id/profile-id force fallback": {"https://stack.grafana.net", "pyro-uid", `{service_name="frontend"}`, "cpu", true},
		"missing host falls back":            {"", "pyro-uid", `{service_name="frontend"}`, "cpu", false},
		"missing datasource UID falls back":  {"https://stack.grafana.net", "", `{service_name="frontend"}`, "cpu", false},
		"missing selector falls back":        {"https://stack.grafana.net", "pyro-uid", "", "cpu", false},
		"missing profile type falls back":    {"https://stack.grafana.net", "pyro-uid", `{service_name="frontend"}`, "", false},
		"malformed selector falls back":      {"https://stack.grafana.net", "pyro-uid", `service_name="frontend"`, "cpu", false},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := pyroscope.ProfilesDrilldownURL(tt.host, tt.uid, tt.selector, tt.profileType, nil, tt.hasUnsupportedDrillDown, time.Time{}, time.Time{})
			assert.False(t, ok)
		})
	}
}
