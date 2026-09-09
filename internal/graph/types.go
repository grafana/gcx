package graph

import (
	"image/color"
	"time"
)

// ChartData represents data ready to be rendered as a chart.
type ChartData struct {
	Title  string
	Series []Series
}

// Series represents a single time series in the chart.
type Series struct {
	Name   string
	Labels map[string]string
	Points []Point
	Color  color.Color // Optional: if set, overrides ColorForIndex for this series
}

// Point represents a single data point.
type Point struct {
	Time  time.Time
	Value float64
}

// PercentageBarItem represents a single item in a percentage bar chart.
type PercentageBarItem struct {
	Name   string  // Label shown to the left of the bar
	Value  float64 // Current value as percentage (0–100)
	Target float64 // Target/objective as percentage (0–100); 0 means no target
}

// IsInstantQuery returns true if all series have exactly one data point
// at the same timestamp (typical of Prometheus instant/vector queries).
func (d *ChartData) IsInstantQuery() bool {
	if d == nil || len(d.Series) == 0 {
		return false
	}

	var commonTime time.Time
	for i, s := range d.Series {
		if len(s.Points) != 1 {
			return false
		}
		if i == 0 {
			commonTime = s.Points[0].Time
		} else if !s.Points[0].Time.Equal(commonTime) {
			return false
		}
	}
	return true
}
