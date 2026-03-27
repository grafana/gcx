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
