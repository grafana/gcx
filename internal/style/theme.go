// Package style provides centralized terminal styling for the gcx CLI.
// All styling is gated on TTY detection — piped and agent-mode output
// remains plain text for backward compatibility.
package style

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/terminal"
	"golang.org/x/term"
)

// Grafana Neon Dark — color palette derived from Grafana's official dark theme.
//
//nolint:gochecknoglobals
var (
	// Semantic colors.
	ColorPrimary = lipgloss.Color("#6E9FFF")
	ColorMuted   = lipgloss.Color("#7D8085")
	ColorBorder  = lipgloss.Color("#44474E")

	// Gradient endpoints (used by the ASCII logo).
	GradientBrandTo    = lipgloss.Color("#EAB839") // Amber
	GradientAccentFrom = lipgloss.Color("#F2495C") // Coral

	// Grafana chart palette (classic series colors).
	ChartPalette = []lipgloss.Color{
		lipgloss.Color("#7EB26D"), // Green
		lipgloss.Color("#EAB839"), // Yellow
		lipgloss.Color("#6ED0E0"), // Cyan
		lipgloss.Color("#EF843C"), // Orange
		lipgloss.Color("#E24D42"), // Red
		lipgloss.Color("#1F78C1"), // Blue
		lipgloss.Color("#BA43A9"), // Purple
		lipgloss.Color("#705DA0"), // Violet
		lipgloss.Color("#508642"), // Dark Green
		lipgloss.Color("#CCA300"), // Gold
	}
)

// disabledOverride allows --no-color to force styling off even on a TTY.
//
//nolint:gochecknoglobals
var disabledOverride atomic.Bool

// SetEnabled controls whether styling is active. Pass false to force plain
// output (used by --no-color). The default is determined by TTY detection.
func SetEnabled(enabled bool) {
	disabledOverride.Store(!enabled)
}

// IsStylingEnabled reports whether the terminal supports styled output.
// Returns false when stdout is piped, agent mode is active, the user
// passed --no-color, or stdout is not a real TTY.
func IsStylingEnabled() bool {
	if disabledOverride.Load() {
		return false
	}
	if terminal.IsPiped() || agent.IsAgentMode() {
		return false
	}
	// Final check: stdout must be a real terminal. This catches test
	// environments where terminal.Detect() was never called.
	stdoutFD, ok := safeFDToInt(os.Stdout.Fd())
	return ok && term.IsTerminal(stdoutFD)
}

// Gradient renders text with a linear color gradient between from and to.
// When styling is disabled, returns the text unchanged.
func Gradient(text string, from, to lipgloss.Color) string {
	if !IsStylingEnabled() || len(text) == 0 {
		return text
	}

	runes := []rune(text)
	n := len(runes)
	if n == 1 {
		return lipgloss.NewStyle().Foreground(from).Render(string(runes))
	}

	r1, g1, b1 := hexToRGB(string(from))
	r2, g2, b2 := hexToRGB(string(to))

	var sb strings.Builder
	for i, r := range runes {
		t := float64(i) / float64(n-1)
		ri := uint8(math.Round(float64(r1) + t*(float64(r2)-float64(r1))))
		gi := uint8(math.Round(float64(g1) + t*(float64(g2)-float64(g1))))
		bi := uint8(math.Round(float64(b1) + t*(float64(b2)-float64(b1))))
		c := lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", ri, gi, bi))
		sb.WriteString(lipgloss.NewStyle().Foreground(c).Render(string(r)))
	}
	return sb.String()
}

// hexToRGB parses a "#RRGGBB" hex string into its components.
func hexToRGB(hex string) (uint8, uint8, uint8) {
	if len(hex) == 7 && hex[0] == '#' {
		hex = hex[1:]
	}
	if len(hex) != 6 {
		return 0, 0, 0
	}
	var r, g, b uint8
	_, _ = fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}
