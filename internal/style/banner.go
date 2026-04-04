package style

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
)

// RenderBanner prints a styled welcome banner for gcx setup.
// No-op when styling is disabled.
func RenderBanner(w io.Writer) {
	if !IsStylingEnabled() {
		return
	}

	title := Gradient("gcx", GradientBrandFrom, GradientBrandTo)
	subtitle := lipgloss.NewStyle().Foreground(ColorMuted).Render("Grafana Cloud CLI — Setup")

	fmt.Fprintln(w, title)
	fmt.Fprintln(w, subtitle)
	fmt.Fprintln(w)
}
