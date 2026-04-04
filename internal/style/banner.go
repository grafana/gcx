package style

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

//nolint:gochecknoglobals
var asciiLogo = []string{
	`                         `,
	`   ██████   ██████ ██   ██`,
	`  ██       ██       ██ ██ `,
	`  ██   ███ ██        ███  `,
	`  ██    ██ ██       ██ ██ `,
	`   ██████   ██████ ██   ██`,
	`                         `,
}

// RenderLogo writes the gcx ASCII logo with a gradient color effect.
// When styling is disabled, returns an empty string.
func RenderLogo() string {
	if !IsStylingEnabled() {
		return ""
	}

	var lines []string
	for _, line := range asciiLogo {
		lines = append(lines, Gradient(line, GradientAccentFrom, GradientBrandTo))
	}

	subtitle := lipgloss.NewStyle().Foreground(ColorMuted).Render("  Grafana Cloud CLI")

	return strings.Join(lines, "\n") + "\n" + subtitle + "\n"
}

// RenderBanner prints a styled welcome banner for gcx setup.
// No-op when styling is disabled.
func RenderBanner(w io.Writer) {
	if !IsStylingEnabled() {
		return
	}

	logo := RenderLogo()
	if logo != "" {
		fmt.Fprintln(w, logo)
	}

	subtitle := lipgloss.NewStyle().Foreground(ColorMuted).Render("Setup Wizard")
	fmt.Fprintln(w, "  "+subtitle)
	fmt.Fprintln(w)
}
