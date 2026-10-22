package graph_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func osFloat(f float64) *float64 { v := f; return &v }

func TestFromOpenSearchResponse_SingleSeries(t *testing.T) {
	resp := &opensearch.MetricsResponse{
		Series: []opensearch.MetricSeries{
			{
				Name:       "count",
				Timestamps: []time.Time{time.UnixMilli(1747000000000).UTC(), time.UnixMilli(1747000060000).UTC()},
				Values:     []*float64{osFloat(10.0), osFloat(20.0)},
			},
		},
	}

	data, err := graph.FromOpenSearchResponse(resp)
	require.NoError(t, err)
	require.Len(t, data.Series, 1)
	assert.Len(t, data.Series[0].Points, 2)
	assert.InDelta(t, 10.0, data.Series[0].Points[0].Value, 0.001)
	assert.Equal(t, time.UnixMilli(1747000000000).UTC(), data.Series[0].Points[0].Time)
}

func TestFromOpenSearchResponse_MultiSeries(t *testing.T) {
	resp := &opensearch.MetricsResponse{
		Series: []opensearch.MetricSeries{
			{Name: "app:frontend", Timestamps: []time.Time{time.UnixMilli(1747000000000).UTC()}, Values: []*float64{osFloat(1.0)}},
			{Name: "app:backend", Timestamps: []time.Time{time.UnixMilli(1747000000000).UTC()}, Values: []*float64{osFloat(2.0)}},
		},
	}

	data, err := graph.FromOpenSearchResponse(resp)
	require.NoError(t, err)
	assert.Len(t, data.Series, 2)
	assert.Equal(t, "app:frontend", data.Series[0].Name)
	assert.Equal(t, "app:backend", data.Series[1].Name)
}

func TestFromOpenSearchResponse_EmptySeries(t *testing.T) {
	resp := &opensearch.MetricsResponse{Series: []opensearch.MetricSeries{}}

	data, err := graph.FromOpenSearchResponse(resp)
	require.NoError(t, err)
	assert.Empty(t, data.Series)
}

func TestFromOpenSearchResponse_NilResponse(t *testing.T) {
	data, err := graph.FromOpenSearchResponse(nil)
	require.NoError(t, err)
	assert.Empty(t, data.Series)
}

func TestFromOpenSearchResponse_AllNilValues(t *testing.T) {
	resp := &opensearch.MetricsResponse{
		Series: []opensearch.MetricSeries{
			{
				Name:       "gap",
				Timestamps: []time.Time{time.UnixMilli(1747000000000).UTC(), time.UnixMilli(1747000060000).UTC()},
				Values:     []*float64{nil, nil},
			},
		},
	}

	data, err := graph.FromOpenSearchResponse(resp)
	require.NoError(t, err)
	// All-nil series produces no points — series is skipped.
	assert.Empty(t, data.Series)
}
