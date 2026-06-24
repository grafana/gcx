package prometheus_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// frameWith builds a single-series Grafana data frame with a time column and a
// number column, the shape Grafana returns for Prometheus queries.
func frameWith(timestampsMs []any, values []any) prometheus.DataFrame {
	return prometheus.DataFrame{
		Schema: prometheus.DataFrameSchema{
			Fields: []prometheus.Field{
				{Name: "Time", Type: "time"},
				{Name: "Value", Type: "number", Labels: map[string]string{"__name__": "up"}},
			},
		},
		Data: prometheus.DataFrameData{
			Values: [][]any{timestampsMs, values},
		},
	}
}

func grafanaResp(frames ...prometheus.DataFrame) *prometheus.GrafanaQueryResponse {
	return &prometheus.GrafanaQueryResponse{
		Results: map[string]prometheus.GrafanaResult{"A": {Frames: frames}},
	}
}

func TestConvertGrafanaResponse_ResultTypeReflectsIntent(t *testing.T) {
	tests := []struct {
		name           string
		resp           *prometheus.GrafanaQueryResponse
		isRange        bool
		wantResultType string
		wantSamples    int
		// assertSample runs against the first sample when wantSamples > 0.
		assertSample func(t *testing.T, s prometheus.Sample)
	}{
		{
			name:           "empty range reports matrix",
			resp:           grafanaResp(),
			isRange:        true,
			wantResultType: "matrix",
			wantSamples:    0,
		},
		{
			name:           "empty instant reports vector",
			resp:           grafanaResp(),
			isRange:        false,
			wantResultType: "vector",
			wantSamples:    0,
		},
		{
			name:           "multi-point range is matrix with Values",
			resp:           grafanaResp(frameWith([]any{1000.0, 2000.0, 3000.0}, []any{1.0, 2.0, 3.0})),
			isRange:        true,
			wantResultType: "matrix",
			wantSamples:    1,
			assertSample: func(t *testing.T, s prometheus.Sample) {
				t.Helper()
				assert.Nil(t, s.Value)
				require.Len(t, s.Values, 3)
				// Milliseconds are converted to Prometheus seconds.
				assert.InDelta(t, 1.0, s.Values[0][0], 1e-9)
				assert.Equal(t, "1", s.Values[0][1])
			},
		},
		{
			// Issue 2 regression guard: a one-point range must still be a matrix.
			name:           "single-point range is matrix with Values",
			resp:           grafanaResp(frameWith([]any{1000.0}, []any{5.0})),
			isRange:        true,
			wantResultType: "matrix",
			wantSamples:    1,
			assertSample: func(t *testing.T, s prometheus.Sample) {
				t.Helper()
				assert.Nil(t, s.Value)
				require.Len(t, s.Values, 1)
				assert.InDelta(t, 1.0, s.Values[0][0], 1e-9)
				assert.Equal(t, "5", s.Values[0][1])
			},
		},
		{
			name:           "single-point instant is vector with Value",
			resp:           grafanaResp(frameWith([]any{1700000000000.0}, []any{1.0})),
			isRange:        false,
			wantResultType: "vector",
			wantSamples:    1,
			assertSample: func(t *testing.T, s prometheus.Sample) {
				t.Helper()
				assert.Nil(t, s.Values)
				require.Len(t, s.Value, 2)
				assert.InDelta(t, 1700000000.0, s.Value[0], 1e-9)
				assert.Equal(t, "1", s.Value[1])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := prometheus.ConvertGrafanaResponse(tt.resp, tt.isRange)

			assert.Equal(t, "success", got.Status)
			assert.Equal(t, tt.wantResultType, got.Data.ResultType)
			require.Len(t, got.Data.Result, tt.wantSamples)
			if tt.wantSamples > 0 && tt.assertSample != nil {
				tt.assertSample(t, got.Data.Result[0])
			}
		})
	}
}
