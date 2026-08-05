package graph

import (
	"github.com/grafana/gcx/internal/query/elasticsearch"
)

// FromElasticsearchResponse converts an Elasticsearch aggregation response to
// ChartData for visualization.
func FromElasticsearchResponse(resp *elasticsearch.MetricsResponse) (*ChartData, error) {
	if resp == nil {
		return &ChartData{}, nil
	}

	data := &ChartData{
		Series: make([]Series, 0, len(resp.Series)),
	}

	for _, s := range resp.Series {
		points := make([]Point, 0, len(s.Timestamps))
		for i, ts := range s.Timestamps {
			if i >= len(s.Values) || s.Values[i] == nil {
				continue
			}
			points = append(points, Point{Time: ts, Value: *s.Values[i]})
		}
		if len(points) == 0 {
			continue
		}
		data.Series = append(data.Series, Series{Name: s.Name, Points: points})
	}

	return data, nil
}
