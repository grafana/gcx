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

// logVolumeBuckets is the target number of time buckets for the log-volume
// histogram, matching the granularity Grafana Explore's own logs-volume
// mini-graph aims for.
const logVolumeBuckets = 40

// FromLokiLogVolumeResponse converts a raw Loki log-lines query response into
// a per-level, bucketed log-volume-over-time chart, mirroring the shape of
// Grafana Explore's own logs volume mini-graph (public/app/features/logs/
// logsModel.ts): one series per detected level, colored via LevelColor.
func FromLokiLogVolumeResponse(resp *loki.QueryResponse) (*ChartData, error) {
	if resp == nil || len(resp.Data.Result) == 0 {
		return &ChartData{}, nil
	}

	type timedEntry struct {
		t     time.Time
		level LogLevel
	}

	var entries []timedEntry
	var minTime, maxTime time.Time
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			t, err := loki.ParseTimestamp(entry.Timestamp)
			if err != nil {
				continue
			}
			level := DetectLevel(stream.Stream, entry)
			entries = append(entries, timedEntry{t: t, level: level})
			if minTime.IsZero() || t.Before(minTime) {
				minTime = t
			}
			if maxTime.IsZero() || t.After(maxTime) {
				maxTime = t
			}
		}
	}
	if len(entries) == 0 {
		return &ChartData{}, nil
	}

	bucketSize := maxTime.Sub(minTime) / logVolumeBuckets
	if bucketSize <= 0 {
		bucketSize = time.Second
	}

	counts := make(map[LogLevel]map[int64]int)
	for _, e := range entries {
		bucket := e.t.Sub(minTime) / bucketSize
		if counts[e.level] == nil {
			counts[e.level] = make(map[int64]int)
		}
		counts[e.level][int64(bucket)]++
	}

	// numBuckets spans bucket indices 0..logVolumeBuckets inclusive (a point
	// at exactly maxTime lands in bucket logVolumeBuckets).
	numBuckets := logVolumeBuckets + 1

	data := &ChartData{
		Title:  "Log volume",
		Series: make([]Series, 0, len(counts)),
	}
	for level, byBucket := range counts {
		// Every level's series is filled across the full bucket range (zero
		// where that level had no lines), not just the buckets it actually
		// hit. A rare level with a single occurrence would otherwise produce
		// a single-point series, which a line chart can't draw a line
		// through — it'd sit correctly in the legend but never appear on the
		// chart itself.
		series := Series{
			Name:   string(level),
			Color:  LevelColor(level),
			Points: make([]Point, 0, numBuckets),
		}
		for bucket := range numBuckets {
			series.Points = append(series.Points, Point{
				Time:  minTime.Add(time.Duration(bucket) * bucketSize),
				Value: float64(byBucket[int64(bucket)]),
			})
		}
		data.Series = append(data.Series, series)
	}
	sort.Slice(data.Series, func(i, j int) bool {
		return data.Series[i].Name < data.Series[j].Name
	})

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
