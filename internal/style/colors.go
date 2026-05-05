package style

import "github.com/charmbracelet/lipgloss"

// Threshold colors used by ColorPercent / ColorCell. These mirror the
// Grafana classic palette: error/critical red, warning yellow, muted gray.
//
//nolint:gochecknoglobals
var (
	colorErrorRed  = lipgloss.Color("#E24D42")
	colorWarnAmber = lipgloss.Color("#EAB839")
)

// ColorPercent applies duration-share color thresholds to a % cell:
//
//	isErr            → bold red (overrides everything)
//	dim              → muted gray
//	pct >= 50        → bold red
//	pct >= 10        → yellow
//	otherwise        → unchanged
//
// Returns text unchanged when styling is disabled.
func ColorPercent(text string, pct float64, dim, isErr bool) string {
	if !IsStylingEnabled() {
		return text
	}
	switch {
	case isErr:
		return lipgloss.NewStyle().Foreground(colorErrorRed).Bold(true).Render(text)
	case dim:
		return lipgloss.NewStyle().Foreground(ColorMuted).Render(text)
	case pct >= 50:
		return lipgloss.NewStyle().Foreground(colorErrorRed).Bold(true).Render(text)
	case pct >= 10:
		return lipgloss.NewStyle().Foreground(colorWarnAmber).Render(text)
	default:
		return text
	}
}

// ColorCell applies row-level error/dim treatment to a non-percent cell:
//
//	isErr → red
//	dim   → muted gray
//	else  → unchanged
//
// Error treatment overrides dim. Returns text unchanged when styling is disabled.
func ColorCell(text string, dim, isErr bool) string {
	if !IsStylingEnabled() {
		return text
	}
	if isErr {
		return lipgloss.NewStyle().Foreground(colorErrorRed).Render(text)
	}
	if dim {
		return lipgloss.NewStyle().Foreground(ColorMuted).Render(text)
	}
	return text
}

// ColorMutedText renders text in muted gray. Used for elements like the
// detached-subtrees divider that should always be dim when styling is on.
// Returns text unchanged when styling is disabled.
func ColorMutedText(text string) string {
	if !IsStylingEnabled() {
		return text
	}
	return lipgloss.NewStyle().Foreground(ColorMuted).Render(text)
}
