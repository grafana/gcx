package prometheus_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCSVRecords(t *testing.T) {
	tests := []struct {
		name    string
		data    prometheus.ResultData
		columns []string
		rows    [][]string
		wantErr string
	}{
		{name: "vector labels preserve case and reserved names", data: prometheus.ResultData{ResultType: "vector", Result: []prometheus.Sample{
			{Metric: map[string]string{"value": "label value", "Job": "upper", "job": "lower"}, Value: []any{1700000000.125, "NaN"}},
			{Metric: map[string]string{"job": "other"}, Value: []any{1700000001.5, "+Inf"}},
		}}, columns: []string{"timestamp_unix_seconds", "value", "label.Job", "label.job", "label.value"}, rows: [][]string{
			{"1700000000.125", "NaN", "upper", "lower", "label value"}, {"1700000001.5", "+Inf", "", "other", ""},
		}},
		{name: "matrix emits every sample", data: prometheus.ResultData{ResultType: "matrix", Result: []prometheus.Sample{{Metric: map[string]string{"job": "api"}, Values: [][]any{{1.25, "2"}, {2.5, "3"}}}}}, columns: []string{"timestamp_unix_seconds", "value", "label.job"}, rows: [][]string{{"1.25", "2", "api"}, {"2.5", "3", "api"}}},
		{name: "matrix retains single-point series", data: prometheus.ResultData{ResultType: "matrix", Result: []prometheus.Sample{
			{Metric: map[string]string{"job": "a"}, Values: [][]any{{1.0, "2"}, {2.0, "3"}}},
			{Metric: map[string]string{"job": "b"}, Value: []any{1.0, "4"}},
		}}, columns: []string{"timestamp_unix_seconds", "value", "label.job"}, rows: [][]string{{"1", "2", "a"}, {"2", "3", "a"}, {"1", "4", "b"}}},
		{name: "empty", data: prometheus.ResultData{ResultType: "vector"}, columns: []string{"timestamp_unix_seconds", "value"}},
		{name: "invalid sample", data: prometheus.ResultData{ResultType: "vector", Result: []prometheus.Sample{{Value: []any{1}}}}, wantErr: "invalid metric sample"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			columns, rows, err := (&prometheus.QueryResponse{Data: tt.data}).CSVRecords()
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.columns, columns)
			assert.Equal(t, tt.rows, rows)
		})
	}
}
