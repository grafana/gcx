package definitions

import (
	"time"

	"github.com/grafana/gcx/internal/graph"
)

// SLOMetricPoint represents a single SLO's metric value for instant chart data.
type SLOMetricPoint struct {
	UUID      string
	Name      string
	Value     float64
	Objective float64
}

// SLOTimeSeriesPoint extends SLOMetricPoint with a timestamp for range chart data.
type SLOTimeSeriesPoint struct {
	SLOMetricPoint

	Time time.Time
}

// FromSLOComplianceSummary converts SLO SLI values into a bar chart (instant).
// Each SLO becomes a series with a single point representing its current SLI value.
func FromSLOComplianceSummary(points []SLOMetricPoint) *graph.ChartData {
	now := time.Now()
	data := &graph.ChartData{
		Title:  "SLO Compliance Summary",
		Series: make([]graph.Series, 0, len(points)),
	}

	for _, pt := range points {
		data.Series = append(data.Series, graph.Series{
			Name: pt.Name,
			Points: []graph.Point{{
				Time:  now,
				Value: pt.Value * 100, // Convert to percentage
			}},
		})
	}

	return data
}

// FromSLOBurnDown converts SLO error budget time series into a line chart (range).
// Each SLO UUID maps to a series of budget values over time.
func FromSLOBurnDown(points map[string][]SLOTimeSeriesPoint) *graph.ChartData {
	data := &graph.ChartData{
		Title:  "SLO Error Budget Burn Down",
		Series: make([]graph.Series, 0, len(points)),
	}

	for _, pts := range points {
		if len(pts) == 0 {
			continue
		}

		series := graph.Series{
			Name:   pts[0].Name,
			Points: make([]graph.Point, len(pts)),
		}
		for i, pt := range pts {
			series.Points[i] = graph.Point{
				Time:  pt.Time,
				Value: pt.Value * 100, // Convert to percentage
			}
		}
		data.Series = append(data.Series, series)
	}

	return data
}

// FromSLOBurnRates converts SLO burn rate values into a bar chart (instant).
func FromSLOBurnRates(points []SLOMetricPoint) *graph.ChartData {
	now := time.Now()
	data := &graph.ChartData{
		Title:  "SLO Burn Rates",
		Series: make([]graph.Series, 0, len(points)),
	}

	for _, pt := range points {
		data.Series = append(data.Series, graph.Series{
			Name: pt.Name,
			Points: []graph.Point{{
				Time:  now,
				Value: pt.Value,
			}},
		})
	}

	return data
}

// FromSLOSLITrend converts SLO SLI time series into a line chart (range).
func FromSLOSLITrend(points map[string][]SLOTimeSeriesPoint) *graph.ChartData {
	data := &graph.ChartData{
		Title:  "SLO SLI Trend",
		Series: make([]graph.Series, 0, len(points)),
	}

	for _, pts := range points {
		if len(pts) == 0 {
			continue
		}

		series := graph.Series{
			Name:   pts[0].Name,
			Points: make([]graph.Point, len(pts)),
		}
		for i, pt := range pts {
			series.Points[i] = graph.Point{
				Time:  pt.Time,
				Value: pt.Value * 100, // Convert to percentage
			}
		}
		data.Series = append(data.Series, series)
	}

	return data
}
