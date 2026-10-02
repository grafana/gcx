package tempo_test

import (
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/datasources/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTracesDrilldownURL_SimpleFilters(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC)

	got, ok := tempo.TracesDrilldownURL("https://stack.grafana.net", "tempo-uid",
		`{ span.http.status_code = 500 && resource.service.name = "checkout" }`, start, end)
	require.True(t, ok)

	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "https", u.Scheme)
	assert.Equal(t, "stack.grafana.net", u.Host)
	assert.Equal(t, "/a/grafana-exploretraces-app/explore", u.Path)

	q := u.Query()
	assert.Equal(t, "tempo-uid", q.Get("var-ds"))
	assert.Equal(t, "1704067200000", q.Get("from"))
	assert.Equal(t, "1704070800000", q.Get("to"))
	assert.Equal(t, "true", q.Get("var-primarySignal"))
	assert.Equal(t, "rate", q.Get("var-metric"))
	assert.Equal(t, "traceList", q.Get("actionView"))
	require.Len(t, q["var-filters"], 2)
	assert.Equal(t, "span.http.status_code|=|500", q["var-filters"][0])
	assert.Equal(t, "resource.service.name|=|checkout", q["var-filters"][1])
}

func TestTracesDrilldownURL_FallbackCases(t *testing.T) {
	tests := map[string]struct {
		host, uid, expr string
	}{
		"unsupported expression falls back": {"https://stack.grafana.net", "tempo-uid", `{ duration > 500ms }`},
		"missing host falls back":           {"", "tempo-uid", `{ span.foo = "bar" }`},
		"missing datasource UID falls back": {"https://stack.grafana.net", "", `{ span.foo = "bar" }`},
		"missing expr falls back":           {"https://stack.grafana.net", "tempo-uid", ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := tempo.TracesDrilldownURL(tt.host, tt.uid, tt.expr, time.Time{}, time.Time{})
			assert.False(t, ok)
		})
	}
}
