package graph

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/NimbleMarkets/ntcharts/barchart"
	"github.com/NimbleMarkets/ntcharts/canvas/runes"
	"github.com/NimbleMarkets/ntcharts/linechart/timeserieslinechart"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// ChartOptions configures chart rendering.
type ChartOptions struct {
	Width    int
	Height   int
	Title    string
	TextOnly bool
	MaxValue *float64 // Optional max value for bar charts (e.g., 100 for percentages)
}

// DefaultChartOptions returns default chart options.
func DefaultChartOptions() ChartOptions {
	width, height := getTerminalSize()
	return ChartOptions{
		Width:  width,
		Height: min(height/2, 20),
	}
}

// RenderPercentageBars renders labeled horizontal bars scaled 0–100%.
// Each item gets a line: name, filled/unfilled bar, value, and optional target.
func RenderPercentageBars(w io.Writer, title string, items []PercentageBarItem, opts ChartOptions) error {
	if len(items) == 0 {
		fmt.Fprintln(w, "No data to display")
		return nil
	}

	if opts.TextOnly {
		return renderPercentageBarsText(w, title, items)
	}

	var sb strings.Builder

	if title != "" {
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7FF"))
		sb.WriteString(titleStyle.Render(title))
		sb.WriteString("\n\n")
	}

	// Find max label width for alignment.
	maxLabelWidth := 0
	for _, item := range items {
		n := min(len(item.Name), 30)
		if n > maxLabelWidth {
			maxLabelWidth = n
		}
	}

	// Compute available bar width.
	// Layout: "  {label}  {bar}  {value}  target: {target}"
	const rightWidth = 30 // enough for "  99.72%  target: 99.50%"
	barWidth := min(80, max(20, opts.Width-maxLabelWidth-4-rightWidth))

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))

	for i, item := range items {
		name := item.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}

		fillRatio := item.Value / 100.0
		fillRatio = max(0, min(1, fillRatio))
		filled := int(float64(barWidth) * fillRatio)
		empty := barWidth - filled

		// Bar color: compliance status (green/yellow/orange/red).
		barColor := ComplianceColor(item.Value, item.Target)
		filledStr := lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("█", filled))
		emptyStr := dimStyle.Render(strings.Repeat("░", empty))

		// Label color: series palette to distinguish items.
		labelColor := ColorForIndex(i)
		labelStr := lipgloss.NewStyle().Foreground(labelColor).Render(fmt.Sprintf("%-*s", maxLabelWidth, name))

		valueStr := fmt.Sprintf("%.2f%%", item.Value)

		targetStr := ""
		if item.Target > 0 {
			targetStr = fmt.Sprintf("  target: %.2f%%", item.Target)
		}

		fmt.Fprintf(&sb, "  %s  %s%s  %s%s\n", labelStr, filledStr, emptyStr, valueStr, targetStr)
	}

	_, err := fmt.Fprint(w, sb.String())
	return err
}

func renderPercentageBarsText(w io.Writer, title string, items []PercentageBarItem) error {
	if title != "" {
		fmt.Fprintf(w, "%s\n\n", title)
	}
	for _, item := range items {
		targetStr := ""
		if item.Target > 0 {
			targetStr = fmt.Sprintf("  (target: %.2f%%)", item.Target)
		}
		fmt.Fprintf(w, "  %s: %.2f%%%s\n", item.Name, item.Value, targetStr)
	}
	return nil
}

// RenderChart auto-selects chart type based on data characteristics.
// For instant queries (single point per series at same timestamp), uses bar chart.
// For range queries (multiple points over time), uses line chart.
func RenderChart(w io.Writer, data *ChartData, opts ChartOptions) error {
	if data.IsInstantQuery() {
		return RenderBarChart(w, data, opts)
	}
	return RenderLineChart(w, data, opts)
}

// RenderBarChart renders instant query data as a horizontal bar chart.
func RenderBarChart(w io.Writer, data *ChartData, opts ChartOptions) error {
	if data == nil || len(data.Series) == 0 {
		fmt.Fprintln(w, "No data to display")
		return nil
	}

	if opts.TextOnly {
		return renderTextFallback(w, data)
	}

	barData := make([]barchart.BarData, 0, len(data.Series))
	for i, series := range data.Series {
		if len(series.Points) == 0 {
			continue
		}
		color := ColorForIndex(i)
		label := series.Name
		if len(label) > 30 {
			label = label[:27] + "..."
		}
		barData = append(barData, barchart.BarData{
			Label: label,
			Values: []barchart.BarValue{{
				Name:  series.Name,
				Value: series.Points[0].Value,
				Style: lipgloss.NewStyle().Foreground(color),
			}},
		})
	}

	// Fixed bar sizing: 2 cells per bar, 1 cell gap, +2 for axis.
	const barWidth = 2
	const barGap = 1
	chartHeight := min(opts.Height, len(barData)*(barWidth+barGap)+2)

	chartOpts := []barchart.Option{
		barchart.WithHorizontalBars(),
		barchart.WithDataSet(barData),
		barchart.WithNoAutoBarWidth(),
		barchart.WithBarWidth(barWidth),
		barchart.WithBarGap(barGap),
	}
	if opts.MaxValue != nil {
		chartOpts = append(chartOpts, barchart.WithMaxValue(*opts.MaxValue))
	}

	bc := barchart.New(opts.Width, chartHeight, chartOpts...)

	var sb strings.Builder

	title := opts.Title
	if title == "" {
		title = data.Title
	}
	if title != "" {
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7FF"))
		sb.WriteString(titleStyle.Render(title))
		sb.WriteString("\n\n")
	}

	bc.Draw()
	sb.WriteString(bc.View())
	sb.WriteString("\n")

	// Legend with values (bar chart doesn't show value labels on bars).
	legend := renderBarLegend(data.Series)
	if legend != "" {
		sb.WriteString("\n")
		sb.WriteString(legend)
	}

	_, err := fmt.Fprint(w, sb.String())
	return err
}

// RenderLineChart renders a line chart to the writer.
func RenderLineChart(w io.Writer, data *ChartData, opts ChartOptions) error {
	if data == nil || len(data.Series) == 0 {
		fmt.Fprintln(w, "No data to display")
		return nil
	}

	if opts.TextOnly {
		return renderTextFallback(w, data)
	}

	// Calculate data bounds
	minTime, maxTime, minY, maxY := calculateBounds(data)
	if minTime.Equal(maxTime) {
		// Single point - expand range slightly
		minTime = minTime.Add(-time.Minute)
		maxTime = maxTime.Add(time.Minute)
	}

	// Determine if multi-day range for label formatting
	isMultiDay := maxTime.Sub(minTime) > 24*time.Hour

	// Styles
	mutedColor := lipgloss.Color("#666666")
	axisStyle := lipgloss.NewStyle().Foreground(mutedColor)
	labelStyle := lipgloss.NewStyle().Foreground(mutedColor)

	// Time formatter
	localTimeFormatter := func(_ int, fval float64) string {
		t := time.Unix(int64(fval), 0)
		if isMultiDay {
			return t.Format("01/02 15:04")
		}
		return t.Format("15:04:05")
	}

	// Convert first series to TimePoints
	firstPoints := convertToTimePoints(data.Series[0].Points)

	// Resolve color for first series: use explicit Color if set, else ColorForIndex.
	firstColor := data.Series[0].Color
	if firstColor == "" {
		firstColor = ColorForIndex(0)
	}

	// Create chart options
	chartOpts := []timeserieslinechart.Option{
		timeserieslinechart.WithYRange(minY, maxY),
		timeserieslinechart.WithTimeRange(minTime, maxTime),
		timeserieslinechart.WithAxesStyles(axisStyle, labelStyle),
		timeserieslinechart.WithXLabelFormatter(localTimeFormatter),
		timeserieslinechart.WithTimeSeries(firstPoints),
		timeserieslinechart.WithStyle(lipgloss.NewStyle().Foreground(firstColor)),
		timeserieslinechart.WithLineStyle(runes.ThinLineStyle),
	}

	// Create chart
	tslc := timeserieslinechart.New(opts.Width-4, opts.Height, chartOpts...)

	// Add additional series
	for i := 1; i < len(data.Series); i++ {
		series := data.Series[i]
		color := series.Color
		if color == "" {
			color = ColorForIndex(i)
		}
		dataSetName := fmt.Sprintf("series%d", i)

		points := convertToTimePoints(series.Points)
		for _, pt := range points {
			tslc.PushDataSet(dataSetName, pt)
		}

		tslc.SetDataSetStyle(dataSetName, lipgloss.NewStyle().Foreground(color))
		tslc.SetDataSetLineStyle(dataSetName, runes.ThinLineStyle)
	}

	// Build output
	var sb strings.Builder

	// Title
	title := opts.Title
	if title == "" {
		title = data.Title
	}
	if title != "" {
		titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00D7FF"))
		sb.WriteString(titleStyle.Render(title))
		sb.WriteString("\n\n")
	}

	// Draw chart
	tslc.DrawXYAxisAndLabel()
	if len(data.Series) > 1 {
		tslc.DrawBrailleAll()
	} else {
		tslc.DrawBraille()
	}

	sb.WriteString(tslc.View())
	sb.WriteString("\n")

	// Legend
	legend := renderLegend(data.Series)
	if legend != "" {
		sb.WriteString("\n")
		sb.WriteString(legend)
	}

	_, err := fmt.Fprint(w, sb.String())
	return err
}

func convertToTimePoints(points []Point) []timeserieslinechart.TimePoint {
	result := make([]timeserieslinechart.TimePoint, len(points))
	for i, pt := range points {
		result[i] = timeserieslinechart.TimePoint{
			Time:  pt.Time.UTC(),
			Value: pt.Value,
		}
	}
	return result
}

func calculateBounds(data *ChartData) (time.Time, time.Time, float64, float64) {
	var minTime, maxTime time.Time
	var minY, maxY float64

	first := true
	for _, series := range data.Series {
		for _, pt := range series.Points {
			if first {
				minTime = pt.Time
				maxTime = pt.Time
				minY = pt.Value
				maxY = pt.Value
				first = false
				continue
			}

			if pt.Time.Before(minTime) {
				minTime = pt.Time
			}
			if pt.Time.After(maxTime) {
				maxTime = pt.Time
			}
			if pt.Value < minY {
				minY = pt.Value
			}
			if pt.Value > maxY {
				maxY = pt.Value
			}
		}
	}

	// Add padding to Y range
	yRange := maxY - minY
	if yRange == 0 {
		yRange = 1
	}
	padding := yRange * 0.1
	minY -= padding
	maxY += padding

	return minTime, maxTime, minY, maxY
}

func renderLegend(series []Series) string {
	if len(series) == 0 {
		return ""
	}

	var legendParts []string
	for i, s := range series {
		// Use the series-specific Color if set; otherwise fall back to ColorForIndex.
		color := s.Color
		if color == "" {
			color = ColorForIndex(i)
		}
		colorBox := lipgloss.NewStyle().Foreground(color).Render("●")
		name := s.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		legendParts = append(legendParts, fmt.Sprintf("%s %s", colorBox, name))
	}

	// Join with spacing
	return strings.Join(legendParts, "  ")
}

func renderBarLegend(series []Series) string {
	if len(series) == 0 {
		return ""
	}

	var legendParts []string
	for i, s := range series {
		if len(s.Points) == 0 {
			continue
		}
		color := ColorForIndex(i)
		colorBox := lipgloss.NewStyle().Foreground(color).Render("●")
		name := s.Name
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		legendParts = append(legendParts, fmt.Sprintf("%s %s: %.2f", colorBox, name, s.Points[0].Value))
	}

	return strings.Join(legendParts, "  ")
}

func renderTextFallback(w io.Writer, data *ChartData) error {
	fmt.Fprintln(w, "Chart data (text fallback - pipe to terminal for graph):")
	fmt.Fprintln(w)

	for i, series := range data.Series {
		fmt.Fprintf(w, "Series %d: %s\n", i+1, series.Name)
		for _, pt := range series.Points {
			fmt.Fprintf(w, "  %s: %.4f\n", pt.Time.Format(time.RFC3339), pt.Value)
		}
		fmt.Fprintln(w)
	}

	return nil
}

func getTerminalSize() (int, int) {
	width := 80
	height := 24

	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		width = w
		height = h
	}

	return width, height
}
