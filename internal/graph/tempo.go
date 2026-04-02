package graph

import (
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/query/tempo"
)

// FromTempoMetricsResponse converts a Tempo metrics query response to ChartData.
func FromTempoMetricsResponse(resp *tempo.MetricsResponse) (*ChartData, error) {
	if resp == nil || len(resp.Series) == 0 {
		return &ChartData{}, nil
	}

	data := &ChartData{
		Series: make([]Series, 0, len(resp.Series)),
	}

	for _, s := range resp.Series {
		series := Series{
			Name:   tempo.FormatMetricsLabels(s.Labels),
			Points: make([]Point, 0),
		}

		if len(s.Samples) > 0 {
			// Range query: iterate over samples.
			for _, sample := range s.Samples {
				ts, err := parseTempoTimestamp(sample.TimestampMs)
				if err != nil {
					continue
				}
				series.Points = append(series.Points, Point{Time: ts, Value: sample.Value})
			}
		} else if s.Value != nil {
			// Instant query: single timestamp + value.
			ts, err := parseTempoTimestamp(s.TimestampMs)
			if err == nil {
				series.Points = append(series.Points, Point{Time: ts, Value: *s.Value})
			}
		}

		if len(series.Points) > 0 {
			data.Series = append(data.Series, series)
		}
	}

	return data, nil
}

// parseTempoTimestamp parses a millisecond timestamp string into a time.Time.
func parseTempoTimestamp(ms string) (time.Time, error) {
	msInt, err := strconv.ParseInt(ms, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, msInt*int64(time.Millisecond)), nil
}
