package graph

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/grafana/gcx/internal/style"
)

// ColorForIndex returns the color for a given series index, cycling through
// the Grafana chart palette.
func ColorForIndex(idx int) lipgloss.Color {
	return style.ChartPalette[idx%len(style.ChartPalette)]
}

// Compliance status colors.
//
//nolint:gochecknoglobals
var (
	colorComplianceOK      = lipgloss.Color("#73BF69") // Green — meeting target
	colorComplianceWarning = lipgloss.Color("#FADE2A") // Yellow — just below target
	colorComplianceDanger  = lipgloss.Color("#FF9830") // Orange — moderately below
	colorComplianceCrit    = lipgloss.Color("#F2495C") // Red — significantly breaching
)

// ComplianceColor returns a color reflecting how close value is to target.
// Both value and target are percentages (0–100).
func ComplianceColor(value, target float64) lipgloss.Color {
	if target <= 0 {
		target = 100 // No target: grade against 100%
	}
	ratio := value / target
	switch {
	case ratio >= 1.0:
		return colorComplianceOK
	case ratio >= 0.99:
		return colorComplianceWarning
	case ratio >= 0.95:
		return colorComplianceDanger
	default:
		return colorComplianceCrit
	}
}
