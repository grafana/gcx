package graph

import (
	"errors"
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/query/pyroscope"
)

// FromPyroscopeResponse converts a Pyroscope query response to ChartData for visualization.
// It extracts the top functions by self-time and renders them as a horizontal bar chart.
func FromPyroscopeResponse(resp *pyroscope.QueryResponse) (*ChartData, error) {
	if resp == nil || resp.Flamegraph == nil {
		return nil, errors.New("no flamegraph data in response")
	}

	topFunctions := pyroscope.ExtractTopFunctions(resp.Flamegraph, 20)

	if len(topFunctions) == 0 {
		return &ChartData{
			Title:  "Top Functions by Sample Count",
			Series: []Series{},
		}, nil
	}

	series := make([]Series, 0, len(topFunctions))
	now := time.Now()

	for _, fn := range topFunctions {
		series = append(series, Series{
			Name: fn.Name,
			Points: []Point{
				{
					Time:  now,
					Value: float64(fn.Self),
				},
			},
		})
	}

	return &ChartData{
		Title:  fmt.Sprintf("Top Functions by Sample Count (total: %d)", resp.Flamegraph.Total),
		Series: series,
	}, nil
}

// FromPyroscopeSeriesResponse converts a SelectSeries response to ChartData for time-series visualization.
func FromPyroscopeSeriesResponse(resp *pyroscope.SelectSeriesResponse) (*ChartData, error) {
	if resp == nil || len(resp.Series) == 0 {
		return &ChartData{
			Title:  "Profile Series",
			Series: []Series{},
		}, nil
	}

	series := make([]Series, 0, len(resp.Series))
	for _, s := range resp.Series {
		name := pyroscope.FormatLabelPairs(s.Labels)
		points := make([]Point, 0, len(s.Points))
		for _, p := range s.Points {
			points = append(points, Point{
				Time:  time.UnixMilli(p.TimestampMs()),
				Value: p.FloatValue(),
			})
		}
		if len(points) > 0 {
			series = append(series, Series{
				Name:   name,
				Points: points,
			})
		}
	}

	return &ChartData{
		Title:  "Profile Series",
		Series: series,
	}, nil
}

// FromTopSeriesResponse converts a TopSeriesResponse to ChartData for bar chart visualization.
func FromTopSeriesResponse(resp *pyroscope.TopSeriesResponse) *ChartData {
	series := make([]Series, 0, len(resp.Series))
	now := time.Now()
	for _, e := range resp.Series {
		name := pyroscope.FormatLabelsMap(e.Labels)
		series = append(series, Series{
			Name: name,
			Points: []Point{
				{Time: now, Value: e.Total},
			},
		})
	}
	return &ChartData{
		Title:  "Top Series by Total",
		Series: series,
	}
}
