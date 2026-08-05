package graph

import (
	"github.com/grafana/gcx/internal/query/cloudmonitoring"
)

// FromCloudMonitoringResponse converts a Google Cloud Monitoring query
// response to ChartData for visualization.
func FromCloudMonitoringResponse(resp *cloudmonitoring.QueryResponse) (*ChartData, error) {
	if resp == nil {
		return &ChartData{}, nil
	}

	data := &ChartData{Series: make([]Series, 0, len(resp.Frames))}
	for _, frame := range resp.Frames {
		points := make([]Point, 0, len(frame.Timestamps))
		for i, ts := range frame.Timestamps {
			if i >= len(frame.Values) || frame.Values[i] == nil {
				continue
			}
			points = append(points, Point{Time: ts, Value: *frame.Values[i]})
		}
		if len(points) == 0 {
			continue
		}
		name := frame.Name
		if name == "" {
			name = formatMetricName(frame.Labels)
		}
		data.Series = append(data.Series, Series{Name: name, Labels: frame.Labels, Points: points})
	}

	return data, nil
}
