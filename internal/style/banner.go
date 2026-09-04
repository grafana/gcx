package style

import (
	"strings"

	"charm.land/lipgloss/v2"
)

//nolint:gochecknoglobals
var asciiLogo = []string{
	`                               `,
	`   █████╗   █████╗██╗  ██╗   `,
	`  ██╔═══╝  ██╔═══╝╚██╗██╔╝   `,
	`  ██║  ███╗██║     ╚███╔╝    `,
	`  ██║   ██║██║     ██╔██╗    `,
	`  ╚██████╔╝╚█████╗██╔╝ ██╗  `,
	`   ╚═════╝  ╚════╝╚═╝  ╚═╝  `,
}

// RenderLogo writes the gcx ASCII logo with a gradient color effect.
// When styling is disabled, returns an empty string.
func RenderLogo() string {
	if !IsStylingEnabled() {
		return ""
	}

	lines := make([]string, 0, len(asciiLogo))
	for _, line := range asciiLogo {
		lines = append(lines, Gradient(line, GradientAccentFrom, GradientBrandTo))
	}

	subtitle := lipgloss.NewStyle().Foreground(ColorMuted).Render("  Grafana CLI")

	return strings.Join(lines, "\n") + "\n" + subtitle + "\n"
}
