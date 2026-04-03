package graph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/query/prometheus"
)

// FromPrometheusResponse converts a Prometheus query response to ChartData.
func FromPrometheusResponse(resp *prometheus.QueryResponse) (*ChartData, error) {
	if resp == nil || len(resp.Data.Result) == 0 {
		return &ChartData{}, nil
	}

	data := &ChartData{
		Series: make([]Series, 0, len(resp.Data.Result)),
	}

	for _, sample := range resp.Data.Result {
		series := Series{
			Name:   formatMetricName(sample.Metric),
			Labels: sample.Metric,
			Points: make([]Point, 0),
		}

		switch resp.Data.ResultType {
		case "vector":
			if len(sample.Value) >= 2 {
				t, v, err := parsePoint(sample.Value[0], sample.Value[1])
				if err != nil {
					continue
				}
				series.Points = append(series.Points, Point{Time: t, Value: v})
			}
		case "matrix":
			for _, vals := range sample.Values {
				if len(vals) >= 2 {
					t, v, err := parsePoint(vals[0], vals[1])
					if err != nil {
						continue
					}
					series.Points = append(series.Points, Point{Time: t, Value: v})
				}
			}
		}

		if len(series.Points) > 0 {
			data.Series = append(data.Series, series)
		}
	}

	return data, nil
}

func parsePoint(tsVal, valVal any) (time.Time, float64, error) {
	var ts time.Time
	var value float64

	switch t := tsVal.(type) {
	case float64:
		ts = time.Unix(int64(t), int64((t-float64(int64(t)))*1e9))
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil {
			return ts, value, err
		}
		ts = time.Unix(int64(f), int64((f-float64(int64(f)))*1e9))
	default:
		return ts, value, fmt.Errorf("unexpected timestamp type: %T", tsVal)
	}

	switch v := valVal.(type) {
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return ts, value, err
		}
		value = f
	case float64:
		value = v
	default:
		return ts, value, fmt.Errorf("unexpected value type: %T", valVal)
	}

	return ts, value, nil
}

func formatMetricName(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}

	name, hasName := labels["__name__"]

	otherLabels := make([]string, 0, len(labels)-1)
	for k, v := range labels {
		if k != "__name__" {
			otherLabels = append(otherLabels, fmt.Sprintf("%s=%q", k, v))
		}
	}
	sort.Strings(otherLabels)

	if hasName {
		if len(otherLabels) == 0 {
			return name
		}
		return name + "{" + strings.Join(otherLabels, ", ") + "}"
	}

	return "{" + strings.Join(otherLabels, ", ") + "}"
}

// FromLokiResponse converts a Loki query response to ChartData.
func FromLokiResponse(resp *loki.QueryResponse) (*ChartData, error) {
	if resp == nil || len(resp.Data.Result) == 0 {
		return &ChartData{}, nil
	}

	data := &ChartData{
		Series: make([]Series, 0, len(resp.Data.Result)),
	}

	for _, stream := range resp.Data.Result {
		series := Series{
			Name:   formatMetricName(stream.Stream),
			Labels: stream.Stream,
			Points: make([]Point, 0),
		}

		for _, vals := range stream.Values {
			if len(vals) >= 2 {
				t, v, err := parseLokiPoint(vals[0], vals[1])
				if err != nil {
					continue
				}
				series.Points = append(series.Points, Point{Time: t, Value: v})
			}
		}

		if len(series.Points) > 0 {
			data.Series = append(data.Series, series)
		}
	}

	return data, nil
}

// FromLokiMetricResponse converts a Loki metric query response (time-series) to ChartData.
func FromLokiMetricResponse(resp *loki.MetricQueryResponse) (*ChartData, error) {
	if resp == nil || len(resp.Data.Result) == 0 {
		return &ChartData{}, nil
	}

	data := &ChartData{
		Series: make([]Series, 0, len(resp.Data.Result)),
	}

	for _, sample := range resp.Data.Result {
		series := Series{
			Name:   formatMetricName(sample.Metric),
			Labels: sample.Metric,
			Points: make([]Point, 0),
		}

		switch resp.Data.ResultType {
		case "vector":
			if len(sample.Value) >= 2 {
				t, v, err := parsePoint(sample.Value[0], sample.Value[1])
				if err == nil {
					series.Points = append(series.Points, Point{Time: t, Value: v})
				}
			}
		case "matrix":
			for _, vals := range sample.Values {
				if len(vals) >= 2 {
					t, v, err := parsePoint(vals[0], vals[1])
					if err == nil {
						series.Points = append(series.Points, Point{Time: t, Value: v})
					}
				}
			}
		}

		if len(series.Points) > 0 {
			data.Series = append(data.Series, series)
		}
	}

	return data, nil
}

func parseLokiPoint(tsVal, valVal string) (time.Time, float64, error) {
	nanos, err := strconv.ParseInt(tsVal, 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to parse timestamp: %w", err)
	}
	ts := time.Unix(0, nanos)

	value, err := strconv.ParseFloat(valVal, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("failed to parse value: %w", err)
	}

	return ts, value, nil
}
