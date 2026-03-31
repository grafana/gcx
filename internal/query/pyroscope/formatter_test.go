package pyroscope_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"testing"

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

func TestFormatSeriesTable(t *testing.T) {
	tests := []struct {
		name     string
		resp     *pyroscope.SelectSeriesResponse
		contains []string
	}{
		{
			name: "single series with points",
			resp: &pyroscope.SelectSeriesResponse{
				Series: []pyroscope.TimeSeries{
					{
						Labels: []pyroscope.LabelPair{{Name: "service_name", Value: "frontend"}},
						Points: []pyroscope.TimePoint{
							tp(100.5, 1711800000000),
							tp(200.3, 1711800060000),
						},
					},
				},
			},
			contains: []string{
				"LABELS", "TIMESTAMP", "VALUE",
				"{service_name=frontend}",
				"100.50",
				"200.30",
			},
		},
		{
			name: "empty response",
			resp: &pyroscope.SelectSeriesResponse{},
			contains: []string{
				"LABELS", "TIMESTAMP", "VALUE",
				"(no series data)",
			},
		},
		{
			name: "multiple labels",
			resp: &pyroscope.SelectSeriesResponse{
				Series: []pyroscope.TimeSeries{
					{
						Labels: []pyroscope.LabelPair{
							{Name: "namespace", Value: "prod"},
							{Name: "service_name", Value: "api"},
						},
						Points: []pyroscope.TimePoint{
							tp(42.0, 1711800000000),
						},
					},
				},
			},
			contains: []string{
				"{namespace=prod, service_name=api}",
				"42.00",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := pyroscope.FormatSeriesTable(&buf, tt.resp)
			require.NoError(t, err)

			output := buf.String()
			for _, s := range tt.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatTopSeriesTable(t *testing.T) {
	tests := []struct {
		name     string
		resp     *pyroscope.TopSeriesResponse
		contains []string
	}{
		{
			name: "ranked by nanoseconds",
			resp: &pyroscope.TopSeriesResponse{
				ProfileType: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				GroupBy:     []string{"service_name"},
				Series: []pyroscope.TopSeriesEntry{
					{Rank: 1, Labels: map[string]string{"service_name": "frontend"}, Total: 112900000000},
					{Rank: 2, Labels: map[string]string{"service_name": "backend"}, Total: 50000000000},
				},
			},
			contains: []string{
				"RANK", "SERVICE_NAME", "TOTAL (NANOSECONDS)",
				"1", "frontend", "1m52.9s",
				"2", "backend", "50s",
			},
		},
		{
			name: "ranked by bytes",
			resp: &pyroscope.TopSeriesResponse{
				ProfileType: "memory:inuse_space:bytes:space:bytes",
				GroupBy:     []string{"service_name"},
				Series: []pyroscope.TopSeriesEntry{
					{Rank: 1, Labels: map[string]string{"service_name": "api"}, Total: 1073741824},
				},
			},
			contains: []string{
				"TOTAL (BYTES)",
				"1.0 GB",
			},
		},
		{
			name: "empty response",
			resp: &pyroscope.TopSeriesResponse{
				ProfileType: "process_cpu:cpu:nanoseconds:cpu:nanoseconds",
				GroupBy:     []string{"service_name"},
			},
			contains: []string{
				"RANK",
				"(no series data)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := pyroscope.FormatTopSeriesTable(&buf, tt.resp)
			require.NoError(t, err)

			output := buf.String()
			for _, s := range tt.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestAggregateTopSeries(t *testing.T) {
	resp := &pyroscope.SelectSeriesResponse{
		Series: []pyroscope.TimeSeries{
			{
				Labels: []pyroscope.LabelPair{{Name: "service_name", Value: "frontend"}},
				Points: []pyroscope.TimePoint{
					tp(100, 1000),
					tp(200, 2000),
				},
			},
			{
				Labels: []pyroscope.LabelPair{{Name: "service_name", Value: "backend"}},
				Points: []pyroscope.TimePoint{
					tp(500, 1000),
				},
			},
		},
	}

	top := pyroscope.AggregateTopSeries(resp, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", []string{"service_name"}, 10)

	assert.Equal(t, "process_cpu:cpu:nanoseconds:cpu:nanoseconds", top.ProfileType)
	assert.Equal(t, []string{"service_name"}, top.GroupBy)
	require.Len(t, top.Series, 2)

	// backend (500) should be ranked first
	assert.Equal(t, 1, top.Series[0].Rank)
	assert.Equal(t, "backend", top.Series[0].Labels["service_name"])
	assert.InDelta(t, 500.0, top.Series[0].Total, 0.001)

	// frontend (100+200=300) should be ranked second
	assert.Equal(t, 2, top.Series[1].Rank)
	assert.Equal(t, "frontend", top.Series[1].Labels["service_name"])
	assert.InDelta(t, 300.0, top.Series[1].Total, 0.001)
}

func TestAggregateTopSeries_Limit(t *testing.T) {
	resp := &pyroscope.SelectSeriesResponse{
		Series: []pyroscope.TimeSeries{
			{Labels: []pyroscope.LabelPair{{Name: "s", Value: "a"}}, Points: []pyroscope.TimePoint{tp(100, 1000)}},
			{Labels: []pyroscope.LabelPair{{Name: "s", Value: "b"}}, Points: []pyroscope.TimePoint{tp(200, 1000)}},
			{Labels: []pyroscope.LabelPair{{Name: "s", Value: "c"}}, Points: []pyroscope.TimePoint{tp(300, 1000)}},
		},
	}

	top := pyroscope.AggregateTopSeries(resp, "test", []string{"s"}, 2)
	assert.Len(t, top.Series, 2)
	assert.Equal(t, "c", top.Series[0].Labels["s"])
	assert.Equal(t, "b", top.Series[1].Labels["s"])
}

func TestFormatSeriesTableWide(t *testing.T) {
	tests := []struct {
		name     string
		resp     *pyroscope.SelectSeriesResponse
		contains []string
	}{
		{
			name: "labels exploded into columns",
			resp: &pyroscope.SelectSeriesResponse{
				Series: []pyroscope.TimeSeries{
					{
						Labels: []pyroscope.LabelPair{
							{Name: "namespace", Value: "prod"},
							{Name: "service_name", Value: "api"},
						},
						Points: []pyroscope.TimePoint{
							tp(100.0, 1711800000000),
						},
					},
				},
			},
			contains: []string{
				"NAMESPACE", "SERVICE_NAME", "TIMESTAMP", "VALUE",
				"prod", "api", "100.00",
			},
		},
		{
			name: "empty response",
			resp: &pyroscope.SelectSeriesResponse{},
			contains: []string{
				"TIMESTAMP", "VALUE",
				"(no series data)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := pyroscope.FormatSeriesTableWide(&buf, tt.resp)
			require.NoError(t, err)

			output := buf.String()
			for _, s := range tt.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}
