package graph_test

import (
	"testing"
	"time"

	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/cloudwatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cwFrame(name string, labels map[string]string, tsMs []int64, vals []*float64) cloudwatch.Frame {
	timestamps := make([]time.Time, len(tsMs))
	for i, ms := range tsMs {
		timestamps[i] = time.UnixMilli(ms).UTC()
	}
	return cloudwatch.Frame{
		Name:       name,
		Labels:     labels,
		Timestamps: timestamps,
		Values:     vals,
	}
}

func cwFloat(f float64) *float64 { v := f; return &v }

func TestFromCloudWatchResponse_SingleSeries(t *testing.T) {
	resp := &cloudwatch.QueryResponse{
		Frames: []cloudwatch.Frame{
			cwFrame("CPUUtilization", nil, []int64{1747000000000, 1747000060000}, []*float64{cwFloat(10.0), cwFloat(20.0)}),
		},
	}

	data, err := graph.FromCloudWatchResponse(resp)
	require.NoError(t, err)
	require.Len(t, data.Series, 1)
	assert.Len(t, data.Series[0].Points, 2)
	assert.InDelta(t, 10.0, data.Series[0].Points[0].Value, 0.001)
	assert.Equal(t, time.UnixMilli(1747000000000).UTC(), data.Series[0].Points[0].Time)
}

func TestFromCloudWatchResponse_MultiSeries(t *testing.T) {
	resp := &cloudwatch.QueryResponse{
		Frames: []cloudwatch.Frame{
			cwFrame("series-a", map[string]string{"InstanceId": "i-abc"}, []int64{1747000000000}, []*float64{cwFloat(1.0)}),
			cwFrame("series-b", map[string]string{"InstanceId": "i-xyz"}, []int64{1747000000000}, []*float64{cwFloat(2.0)}),
		},
	}

	data, err := graph.FromCloudWatchResponse(resp)
	require.NoError(t, err)
	assert.Len(t, data.Series, 2)
	assert.Equal(t, "series-a", data.Series[0].Name)
	assert.Equal(t, "series-b", data.Series[1].Name)
}

func TestFromCloudWatchResponse_LabelPopulation(t *testing.T) {
	labels := map[string]string{"InstanceId": "i-abc"}
	resp := &cloudwatch.QueryResponse{
		Frames: []cloudwatch.Frame{
			cwFrame("", labels, []int64{1747000000000}, []*float64{cwFloat(5.0)}),
		},
	}

	data, err := graph.FromCloudWatchResponse(resp)
	require.NoError(t, err)
	require.Len(t, data.Series, 1)
	assert.Equal(t, labels, data.Series[0].Labels)
}

func TestFromCloudWatchResponse_EmptyFrames(t *testing.T) {
	resp := &cloudwatch.QueryResponse{Frames: []cloudwatch.Frame{}}

	data, err := graph.FromCloudWatchResponse(resp)
	require.NoError(t, err)
	assert.Empty(t, data.Series)
}

func TestFromCloudWatchResponse_NilResponse(t *testing.T) {
	data, err := graph.FromCloudWatchResponse(nil)
	require.NoError(t, err)
	assert.Empty(t, data.Series)
}

func TestFromCloudWatchResponse_AllNilValues(t *testing.T) {
	resp := &cloudwatch.QueryResponse{
		Frames: []cloudwatch.Frame{
			cwFrame("test", nil, []int64{1747000000000, 1747000060000}, []*float64{nil, nil}),
		},
	}

	data, err := graph.FromCloudWatchResponse(resp)
	require.NoError(t, err)
	// All-nil frame produces no series points — frame is skipped.
	assert.Empty(t, data.Series)
}
