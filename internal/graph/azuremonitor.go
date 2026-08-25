package graph

import (
	"github.com/grafana/gcx/internal/query/azuremonitor"
)

// FromAzureMonitorResponse converts an Azure Monitor query response to ChartData for visualization.
func FromAzureMonitorResponse(resp *azuremonitor.QueryResponse) (*ChartData, error) {
	data := &ChartData{Series: []Series{}}
	if resp == nil {
		return data, nil
	}

	for _, frame := range resp.Frames {
		series := azureMonitorSeries(frame)
		if len(series.Points) > 0 {
			data.Series = append(data.Series, series)
		}
	}

	return data, nil
}

// azureMonitorSeries converts a single Azure Monitor frame to a chart series,
// skipping nil values (sparse-metric gaps).
func azureMonitorSeries(frame azuremonitor.Frame) Series {
	name := frame.Name
	if name == "" {
		name = formatMetricName(frame.Labels)
	}

	points := make([]Point, 0, len(frame.Timestamps))
	for i, ts := range frame.Timestamps {
		if i >= len(frame.Values) || frame.Values[i] == nil {
			continue
		}
		points = append(points, Point{Time: ts, Value: *frame.Values[i]})
	}

	return Series{Name: name, Labels: frame.Labels, Points: points}
}
