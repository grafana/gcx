package loki_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVRecords(t *testing.T) {
	resp := &loki.QueryResponse{Data: loki.QueryResultData{Result: []loki.StreamEntry{{
		Stream: map[string]string{"app": "api", "__name__": "logs"},
		Values: []loki.LogEntry{
			{Timestamp: "1775637286777686890", Line: "msg=\"original, line\"\nnext", StructuredMetadata: map[string]string{"app": "meta"}, Parsed: map[string]string{"app": "parsed"}},
			{Timestamp: "1775637286777686891", Line: "second"},
		},
	}}}}
	columns, rows, err := resp.CSVRecords()
	require.NoError(t, err)
	assert.Equal(t, []string{"timestamp_unix_nano", "message", "label.__name__", "label.app", "metadata.app", "parsed.app"}, columns)
	assert.Equal(t, [][]string{
		{"1775637286777686890", "msg=\"original, line\"\nnext", "logs", "api", "meta", "parsed"},
		{"1775637286777686891", "second", "logs", "api", "", ""},
	}, rows)
	columns, rows, err = (&loki.QueryResponse{}).CSVRecords()
	require.NoError(t, err)
	assert.Equal(t, []string{"timestamp_unix_nano", "message"}, columns)
	assert.Empty(t, rows)
}

func TestMetricCSVRecords(t *testing.T) {
	for _, resultType := range []string{"vector", "matrix"} {
		t.Run(resultType, func(t *testing.T) {
			point := []any{1.25, "3.5"}
			resp := &loki.MetricQueryResponse{Data: loki.MetricQueryData{ResultType: resultType, Result: []loki.MetricQuerySample{{Metric: map[string]string{"app": "api"}, Value: point, Values: [][]any{point}}}}}
			columns, rows, err := resp.CSVRecords()
			require.NoError(t, err)
			assert.Equal(t, []string{"timestamp_unix_seconds", "value", "label.app"}, columns)
			assert.Equal(t, [][]string{{"1.25", "3.5", "api"}}, rows)
		})
	}
}

func TestMetricCSVMixedSeries(t *testing.T) {
	resp := &loki.MetricQueryResponse{Data: loki.MetricQueryData{ResultType: "matrix", Result: []loki.MetricQuerySample{
		{Metric: map[string]string{"app": "a"}, Values: [][]any{{1.0, "2"}, {2.0, "3"}}},
		{Metric: map[string]string{"app": "b"}, Value: []any{1.0, "4"}},
	}}}
	_, rows, err := resp.CSVRecords()
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"1", "2", "a"}, {"2", "3", "a"}, {"1", "4", "b"}}, rows)
}
