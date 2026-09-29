package loki

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestLogSelector(t *testing.T) {
	for _, tt := range []struct{ query, selector string }{
		{`{app="test"}`, `{app="test"}`},
		{`{app="test"} |= "error" | json`, `{app="test"}`},
		{"{app=\"te}st\"} |~ `[{()]`", `{app="te}st"}`},
		{`{app="te\"st"} != "debug"`, `{app="te\"st"}`},
		{`count_over_time({app="test"}[1h])`, ""},
		{`{app="test"} or {app="other"}`, ""},
		{`{app="test"}[1h]`, ""},
		{`{app="test"} # comment`, ""},
		{`{app="test"} |~ "unterminated`, ""},
		{`{app="test"} | x > 0 or (y < 1)`, ""},
	} {
		t.Run(tt.query, func(t *testing.T) {
			got, ok := logSelector(tt.query)
			assert.Equal(t, tt.selector, got)
			assert.Equal(t, tt.selector != "", ok)
		})
	}
}

func TestEstimateCancellationAndEscapedUID(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Contains(t, r.URL.EscapedPath(), "/uid/a%2Fb/resources/index/stats")
		_, _ = w.Write([]byte(`{"bytes":123}`))
	}))
	t.Cleanup(server.Close)
	client, err := NewClient(config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}})
	require.NoError(t, err)
	req := QueryRequest{Query: `{app="test"}`, Start: time.Now().Add(-time.Hour), End: time.Now()}
	e, err := client.EstimateScan(t.Context(), "a/b", req)
	require.NoError(t, err)
	require.NotNil(t, e.Bytes)
	assert.Equal(t, int64(123), *e.Bytes)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.EstimateScan(ctx, "a/b", req)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, calls)
}

func TestQueryWideScanStatsAreNotSummedAcrossFrames(t *testing.T) {
	frame := DataFrame{Schema: DataFrameSchema{Meta: &FrameMeta{Stats: []FrameStat{
		{DisplayName: "Summary: total bytes processed", Value: 1234},
		{DisplayName: "Summary: exec time", Value: 2.5},
	}}}}
	resp := &GrafanaQueryResponse{Results: map[string]GrafanaResult{"A": {Frames: []DataFrame{frame, frame}}}}
	logs := convertGrafanaResponse(resp)
	metrics := convertMetricResponse(resp)
	require.NotNil(t, logs.Data.Stats)
	require.NotNil(t, metrics.Data.Stats)
	assert.Equal(t, int64(1234), logs.Data.Stats.Summary.TotalBytesProcessed)
	assert.Equal(t, int64(1234), metrics.Data.Stats.Summary.TotalBytesProcessed)
	assert.InDelta(t, 2.5, metrics.Data.Stats.Summary.ExecTime, 0.0001)
	assert.Nil(t, extractStats([]FrameStat{{DisplayName: "Summary: exec time", Value: 2.5}}), "absent byte statistics must not become zero")
}
