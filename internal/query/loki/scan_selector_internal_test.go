package loki

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestScanSelector(t *testing.T) {
	for _, tt := range []struct {
		query            string
		lookback, offset time.Duration
		ok               bool
	}{
		{`count_over_time({app="test"}[5m])`, 5 * time.Minute, 0, true},
		{`sum by (user) (count_over_time({app="test"} |= "enrichment failed" | logfmt [5m]))`, 5 * time.Minute, 0, true},
		{`sum(rate({app="test"}[1h30m] offset 1d)) by (app)`, 90 * time.Minute, 24 * time.Hour, true},
		{`quantile_over_time(0.99, {app="test"} | json | unwrap duration_seconds(latency) | __error__="" [5m]) by (app)`, 5 * time.Minute, 0, true},
		{"sum(rate({app=\"test\"} |~ `[{()] offset 9y` [5m]))", 5 * time.Minute, 0, true},
		{`rate({app="test"} |= "escaped \" offset [1y]" [5m])`, 5 * time.Minute, 0, true},
		{`rate({app="test"}[5m] |= "error")`, 5 * time.Minute, 0, true},
		{`rate({app="test"}[5m]) / 2`, 5 * time.Minute, 0, true},
		{`rate({app="test"}[5m]) / rate({app="other"}[5m])`, 0, 0, false},
		{`rate({app="test"}[5m]) + rate({app="test"}[5m])`, 0, 0, false},
		{`sum_over_time(rate({app="test"}[5m])[1h:1m])`, 0, 0, false},
		{`rate({app="test"}[$__auto])`, 0, 0, false},
		{`rate({app="test"}[5m] offset -1h)`, 0, 0, false},
		{`rate({app="test"}[5m] offset 1h offset 2h)`, 0, 0, false},
		{`rate({app="test"}[5m]) offset 1h`, 0, 0, false},
		{`rate({app="test"}[5m] @ 1234)`, 0, 0, false},
		{"rate({app=\"test\"}[5m]) # offset\n", 0, 0, false},
		{`rate({app="test"} |= "unterminated [5m])`, 0, 0, false},
		{`rate({app="test"}[0m])`, 0, 0, false},
		{`rate({app="test"}[999999999999999999999y])`, 0, 0, false},
		{`rate({app="test"}[5m] offset 999999999999999999999y)`, 0, 0, false},
		{`rate({app="test"}[5m]`, 0, 0, false},
		{`rate({app="test"}[5m))`, 0, 0, false},
		{`{app="test"}[5m]`, 0, 0, false},
	} {
		t.Run(tt.query, func(t *testing.T) {
			selector, lookback, offset, ok := scanSelector(tt.query)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, `{app="test"}`, selector)
				assert.Equal(t, tt.lookback, lookback)
				assert.Equal(t, tt.offset, offset)
			}
		})
	}
}

func TestMetricScanInterval(t *testing.T) {
	end := time.Date(2026, 9, 29, 7, 15, 0, 0, time.UTC)
	for _, tt := range []struct {
		name, expr string
		instant    bool
		start, end time.Time
	}{
		{"RCA range", `sum by (user) (count_over_time({app="test"} |= "enrichment failed" | logfmt [5m]))`, false, end.Add(-10 * time.Minute), end},
		{"range offset", `rate({app="test"}[5m] offset 1h)`, false, end.Add(-70 * time.Minute), end.Add(-time.Hour)},
		{"instant", `count_over_time({app="test"}[5m])`, true, end.Add(-5 * time.Minute), end},
		{"instant offset", `rate({app="test"}[5m] offset 1h)`, true, end.Add(-65 * time.Minute), end.Add(-time.Hour)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/api/datasources/uid/test/resources/index/stats", r.URL.Path)
				assert.Equal(t, `{app="test"}`, r.URL.Query().Get("query"))
				assert.Equal(t, strconv.FormatInt(tt.start.UnixNano(), 10), r.URL.Query().Get("start"))
				assert.Equal(t, strconv.FormatInt(tt.end.UnixNano(), 10), r.URL.Query().Get("end"))
				_, _ = w.Write([]byte(`{"bytes":405433264}`))
			}))
			t.Cleanup(server.Close)
			client, err := NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
			require.NoError(t, err)
			req := QueryRequest{Query: tt.expr, EvaluationTime: end}
			if !tt.instant {
				req.Start, req.End = end.Add(-5*time.Minute), end
			}
			estimate, err := client.EstimateScan(t.Context(), "test", req)
			require.NoError(t, err)
			require.NotNil(t, estimate.Bytes)
			assert.Equal(t, int64(405433264), *estimate.Bytes)
			assert.Equal(t, tt.start, *estimate.ScanStart)
			assert.Equal(t, tt.end, *estimate.ScanEnd)
			assert.Equal(t, end, estimate.End)
			wantStart := req.Start
			if tt.instant {
				wantStart = end
			}
			assert.Equal(t, wantStart, estimate.Start)
			assert.Equal(t, 1, calls)
			body, err := client.buildQueryBody("test", req, false)
			require.NoError(t, err)
			var wire struct {
				From    string `json:"from"`
				To      string `json:"to"`
				Queries []struct {
					Expr    string `json:"expr"`
					Instant bool   `json:"instant"`
				} `json:"queries"`
			}
			require.NoError(t, json.Unmarshal(body, &wire))
			assert.Equal(t, tt.expr, wire.Queries[0].Expr)
			assert.Equal(t, strconv.FormatInt(end.UnixMilli(), 10), wire.To)
			if tt.instant {
				wantStart = end.Add(-time.Minute)
			}
			assert.Equal(t, strconv.FormatInt(wantStart.UnixMilli(), 10), wire.From)
			assert.Equal(t, tt.instant, wire.Queries[0].Instant)
		})
	}
}
