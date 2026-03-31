package graph_test

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/grafana/gcx/internal/graph"
	"github.com/grafana/gcx/internal/query/pyroscope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tp(value float64, timestamp int64) pyroscope.TimePoint {
	return pyroscope.TimePoint{
		Value:     json.Number(strconv.FormatFloat(value, 'g', -1, 64)),
		Timestamp: json.Number(strconv.FormatInt(timestamp, 10)),
	}
}

func TestFromPyroscopeSeriesResponse(t *testing.T) {
	tests := []struct {
		name          string
		resp          *pyroscope.SelectSeriesResponse
		wantSeries    int
		wantTitle     string
		wantIsInstant bool
		wantFirstName string
		wantFirstPts  int
	}{
		{
			name: "single series with multiple points",
			resp: &pyroscope.SelectSeriesResponse{
				Series: []pyroscope.TimeSeries{
					{
						Labels: []pyroscope.LabelPair{{Name: "service_name", Value: "frontend"}},
						Points: []pyroscope.TimePoint{
							tp(100.0, 1711800000000),
							tp(200.0, 1711800060000),
							tp(150.0, 1711800120000),
						},
					},
				},
			},
			wantSeries:    1,
			wantTitle:     "Profile Series",
			wantIsInstant: false,
			wantFirstName: "{service_name=frontend}",
			wantFirstPts:  3,
		},
		{
			name: "multiple series",
			resp: &pyroscope.SelectSeriesResponse{
				Series: []pyroscope.TimeSeries{
					{
						Labels: []pyroscope.LabelPair{{Name: "namespace", Value: "prod"}},
						Points: []pyroscope.TimePoint{
							tp(100.0, 1711800000000),
						},
					},
					{
						Labels: []pyroscope.LabelPair{{Name: "namespace", Value: "staging"}},
						Points: []pyroscope.TimePoint{
							tp(50.0, 1711800000000),
						},
					},
				},
			},
			wantSeries:    2,
			wantTitle:     "Profile Series",
			wantIsInstant: true,
			wantFirstName: "{namespace=prod}",
			wantFirstPts:  1,
		},
		{
			name:          "empty response",
			resp:          &pyroscope.SelectSeriesResponse{},
			wantSeries:    0,
			wantTitle:     "Profile Series",
			wantIsInstant: false,
		},
		{
			name:          "nil response",
			resp:          nil,
			wantSeries:    0,
			wantTitle:     "Profile Series",
			wantIsInstant: false,
		},
		{
			name: "no labels",
			resp: &pyroscope.SelectSeriesResponse{
				Series: []pyroscope.TimeSeries{
					{
						Labels: nil,
						Points: []pyroscope.TimePoint{
							tp(42.0, 1711800000000),
						},
					},
				},
			},
			wantSeries:    1,
			wantTitle:     "Profile Series",
			wantIsInstant: true,
			wantFirstName: "{}",
			wantFirstPts:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chartData, err := graph.FromPyroscopeSeriesResponse(tt.resp)
			require.NoError(t, err)
			require.NotNil(t, chartData)

			assert.Equal(t, tt.wantTitle, chartData.Title)
			assert.Len(t, chartData.Series, tt.wantSeries)

			if tt.wantSeries > 0 {
				assert.Equal(t, tt.wantFirstName, chartData.Series[0].Name)
				assert.Len(t, chartData.Series[0].Points, tt.wantFirstPts)
			}

			if tt.wantSeries > 0 {
				assert.Equal(t, tt.wantIsInstant, chartData.IsInstantQuery())
			}
		})
	}
}

func TestFromTopSeriesResponse(t *testing.T) {
	resp := &pyroscope.TopSeriesResponse{
		ProfileType: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
		GroupBy:     []string{"service_name"},
		Series: []pyroscope.TopSeriesEntry{
			{Rank: 1, Labels: map[string]string{"service_name": "frontend"}, Total: 100},
			{Rank: 2, Labels: map[string]string{"service_name": "backend"}, Total: 50},
		},
	}

	chartData := graph.FromTopSeriesResponse(resp)
	assert.Equal(t, "Top Series by Total", chartData.Title)
	assert.Len(t, chartData.Series, 2)
	assert.Equal(t, "{service_name=frontend}", chartData.Series[0].Name)
	assert.InDelta(t, 100.0, chartData.Series[0].Points[0].Value, 0.001)
	// All series have 1 point at same time → bar chart
	assert.True(t, chartData.IsInstantQuery())
}

func TestFromTopSeriesResponse_Empty(t *testing.T) {
	resp := &pyroscope.TopSeriesResponse{}
	chartData := graph.FromTopSeriesResponse(resp)
	assert.Equal(t, "Top Series by Total", chartData.Title)
	assert.Empty(t, chartData.Series)
}
