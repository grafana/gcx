package pyroscope_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAnomaliesResult(t *testing.T) {
	from := time.Unix(1000, 0).UTC()
	to := time.Unix(2000, 0).UTC()

	resp := &pyroscope.QueryAnomaliesResponse{
		StacktraceAnomalies: []pyroscope.StacktraceAnomaly{
			{
				ProfileID: "least-anomalous",
				Timestamp: num(1500100),
				Score:     -0.1,
				Labels: []pyroscope.LabelPair{
					{Name: "service_name", Value: "frontend"},
					{Name: "__period_type__", Value: "cpu"}, // internal label should be filtered
				},
			},
			{
				ProfileID: "most-anomalous",
				Timestamp: num(1500200),
				Score:     -0.9,
				Labels: []pyroscope.LabelPair{
					{Name: "service_name", Value: "backend"},
				},
			},
		},
	}

	result := pyroscope.BuildAnomaliesResult(resp, from, to, 0)

	assert.Equal(t, from, result.From)
	assert.Equal(t, to, result.To)
	require.Len(t, result.Anomalies, 2)

	// Most anomalous (lowest score) sorts first.
	assert.Equal(t, "most-anomalous", result.Anomalies[0].ProfileID)
	assert.InDelta(t, -0.9, result.Anomalies[0].Score, 0.0001)
	assert.Equal(t, time.UnixMilli(1500200).UTC(), result.Anomalies[0].Timestamp)
	assert.Equal(t, map[string]string{"service_name": "backend"}, result.Anomalies[0].Labels)

	assert.Equal(t, "least-anomalous", result.Anomalies[1].ProfileID)
	assert.NotContains(t, result.Anomalies[1].Labels, "__period_type__")
}

func TestBuildAnomaliesResult_TopN(t *testing.T) {
	from := time.Unix(1000, 0).UTC()
	to := time.Unix(2000, 0).UTC()

	resp := &pyroscope.QueryAnomaliesResponse{
		StacktraceAnomalies: []pyroscope.StacktraceAnomaly{
			{ProfileID: "least-anomalous", Timestamp: num(1500100), Score: -0.1},
			{ProfileID: "most-anomalous", Timestamp: num(1500200), Score: -0.9},
			{ProfileID: "mid-anomalous", Timestamp: num(1500300), Score: -0.5},
		},
	}

	result := pyroscope.BuildAnomaliesResult(resp, from, to, 2)

	require.Len(t, result.Anomalies, 2)
	assert.Equal(t, "most-anomalous", result.Anomalies[0].ProfileID)
	assert.Equal(t, "mid-anomalous", result.Anomalies[1].ProfileID)
}

func TestFormatAnomaliesTable(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var buf bytes.Buffer
		result := &pyroscope.AnomaliesResult{}
		require.NoError(t, pyroscope.FormatAnomaliesTable(&buf, result, 3))
		assert.Contains(t, buf.String(), "no anomalies")
	})

	t.Run("populated", func(t *testing.T) {
		var buf bytes.Buffer
		result := &pyroscope.AnomaliesResult{
			Anomalies: []pyroscope.Anomaly{
				{
					ProfileID: "11111111-1111-1111-1111-111111111111",
					Timestamp: time.Unix(1600000, 0).UTC(),
					Score:     -0.42,
					Labels:    map[string]string{"service_name": "frontend"},
				},
			},
		}
		require.NoError(t, pyroscope.FormatAnomaliesTable(&buf, result, 3))
		out := buf.String()
		assert.Contains(t, out, "11111111-1111-1111-1111-111111111111")
		assert.Contains(t, out, "-0.4200")
		assert.Contains(t, out, "frontend")
	})
}
