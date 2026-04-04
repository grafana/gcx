package style

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
)

// RenderContextBadge writes a subtle one-line context indicator to w.
// No-op when styling is disabled. Should be called with stderr to avoid
// polluting piped stdout.
func RenderContextBadge(w io.Writer, name, server string) {
	if !IsStylingEnabled() || name == "" {
		return
	}

	muted := lipgloss.NewStyle().Foreground(ColorMuted)
	accent := lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)

	badge := muted.Render("◆ context: ") +
		accent.Render(name)

	if server != "" {
		badge += muted.Render(" — ") + muted.Render(server)
	}

	fmt.Fprintln(w, badge)
}
