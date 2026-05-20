package graph

import (
	"github.com/grafana/gcx/internal/query/cloudwatch"
)

// FromCloudWatchResponse converts a CloudWatch query response to ChartData for visualization.
func FromCloudWatchResponse(resp *cloudwatch.QueryResponse) (*ChartData, error) {
	if resp == nil || len(resp.Frames) == 0 {
		return &ChartData{}, nil
	}

	data := &ChartData{
		Series: make([]Series, 0, len(resp.Frames)),
	}

	for _, frame := range resp.Frames {
		label := frame.Name
		if label == "" {
			label = formatMetricName(frame.Labels)
		}

		points := make([]Point, 0, len(frame.Timestamps))
		for i, ts := range frame.Timestamps {
			if i < len(frame.Values) && frame.Values[i] != nil {
				points = append(points, Point{
					Time:  ts,
					Value: *frame.Values[i],
				})
			}
		}

		if len(points) > 0 {
			data.Series = append(data.Series, Series{
				Name:   label,
				Labels: frame.Labels,
				Points: points,
			})
		}
	}

	return data, nil
}
