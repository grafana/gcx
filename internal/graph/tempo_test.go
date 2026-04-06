package graph_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/tempo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeLabel(key, val string) tempo.MetricsLabel {
	return tempo.MetricsLabel{
		Key:   key,
		Value: map[string]any{"stringValue": val},
	}
}

func TestFromTempoMetricsResponse_Nil(t *testing.T) {
	data, err := graph.FromTempoMetricsResponse(nil)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Empty(t, data.Series)
}

func TestFromTempoMetricsResponse_Empty(t *testing.T) {
	resp := &tempo.MetricsResponse{
		Series: []tempo.MetricsSeries{},
	}
	data, err := graph.FromTempoMetricsResponse(resp)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Empty(t, data.Series)
}

func TestFromTempoMetricsResponse_Range(t *testing.T) {
	resp := &tempo.MetricsResponse{
		Series: []tempo.MetricsSeries{
			{
				Labels: []tempo.MetricsLabel{
					makeLabel("service", "frontend"),
				},
				Samples: []tempo.MetricsSample{
					{TimestampMs: "1711800000000", Value: 10.5},
					{TimestampMs: "1711800060000", Value: 20.0},
					{TimestampMs: "1711800120000", Value: 15.3},
				},
			},
			{
				Labels: []tempo.MetricsLabel{
					makeLabel("service", "backend"),
				},
				Samples: []tempo.MetricsSample{
					{TimestampMs: "1711800000000", Value: 5.0},
					{TimestampMs: "1711800060000", Value: 8.0},
				},
			},
		},
	}

	data, err := graph.FromTempoMetricsResponse(resp)
	require.NoError(t, err)
	require.Len(t, data.Series, 2)

	// First series: frontend
	s0 := data.Series[0]
	assert.Equal(t, `{service="frontend"}`, s0.Name)
	require.Len(t, s0.Points, 3)
	assert.Equal(t, time.Unix(0, 1711800000000*int64(time.Millisecond)), s0.Points[0].Time)
	assert.InDelta(t, 10.5, s0.Points[0].Value, 0.001)
	assert.InDelta(t, 20.0, s0.Points[1].Value, 0.001)
	assert.InDelta(t, 15.3, s0.Points[2].Value, 0.001)

	// Second series: backend
	s1 := data.Series[1]
	assert.Equal(t, `{service="backend"}`, s1.Name)
	require.Len(t, s1.Points, 2)
	assert.InDelta(t, 5.0, s1.Points[0].Value, 0.001)
	assert.InDelta(t, 8.0, s1.Points[1].Value, 0.001)
}

func TestFromTempoMetricsResponse_Instant(t *testing.T) {
	val1 := 42.0
	val2 := 99.5
	resp := &tempo.MetricsResponse{
		Series: []tempo.MetricsSeries{
			{
				Labels:      []tempo.MetricsLabel{makeLabel("op", "read")},
				TimestampMs: "1711800000000",
				Value:       &val1,
			},
			{
				Labels:      []tempo.MetricsLabel{makeLabel("op", "write")},
				TimestampMs: "1711800000000",
				Value:       &val2,
			},
		},
	}

	data, err := graph.FromTempoMetricsResponse(resp)
	require.NoError(t, err)
	require.Len(t, data.Series, 2)

	// Each series should have exactly one point.
	assert.Len(t, data.Series[0].Points, 1)
	assert.Len(t, data.Series[1].Points, 1)

	assert.Equal(t, `{op="read"}`, data.Series[0].Name)
	assert.InDelta(t, 42.0, data.Series[0].Points[0].Value, 0.001)

	assert.Equal(t, `{op="write"}`, data.Series[1].Name)
	assert.InDelta(t, 99.5, data.Series[1].Points[0].Value, 0.001)

	expectedTime := time.Unix(0, 1711800000000*int64(time.Millisecond))
	assert.Equal(t, expectedTime, data.Series[0].Points[0].Time)
	assert.Equal(t, expectedTime, data.Series[1].Points[0].Time)

	// Same timestamp for all single-point series: IsInstantQuery should be true.
	assert.True(t, data.IsInstantQuery())
}

func TestFromTempoMetricsResponse_Mixed(t *testing.T) {
	instantVal := 77.7
	resp := &tempo.MetricsResponse{
		Series: []tempo.MetricsSeries{
			{
				// Range series with Samples.
				Labels: []tempo.MetricsLabel{makeLabel("kind", "range")},
				Samples: []tempo.MetricsSample{
					{TimestampMs: "1711800000000", Value: 1.0},
					{TimestampMs: "1711800060000", Value: 2.0},
				},
			},
			{
				// Instant series with Value.
				Labels:      []tempo.MetricsLabel{makeLabel("kind", "instant")},
				TimestampMs: "1711800000000",
				Value:       &instantVal,
			},
			{
				// Series with neither Samples nor Value -- should be excluded.
				Labels: []tempo.MetricsLabel{makeLabel("kind", "empty")},
			},
		},
	}

	data, err := graph.FromTempoMetricsResponse(resp)
	require.NoError(t, err)

	// The empty series should be dropped.
	require.Len(t, data.Series, 2)

	assert.Equal(t, `{kind="range"}`, data.Series[0].Name)
	assert.Len(t, data.Series[0].Points, 2)

	assert.Equal(t, `{kind="instant"}`, data.Series[1].Name)
	require.Len(t, data.Series[1].Points, 1)
	assert.InDelta(t, 77.7, data.Series[1].Points[0].Value, 0.001)
}
