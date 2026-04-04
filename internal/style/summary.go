package style

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderSummary writes an operation summary. When styling is enabled it renders
// a bordered panel with colored counts; otherwise it prints the legacy one-liner.
func RenderSummary(w io.Writer, operation string, success, failed, skipped int) {
	if !IsStylingEnabled() {
		renderSummaryPlain(w, operation, success, failed, skipped)
		return
	}
	renderSummaryStyled(w, operation, success, failed, skipped)
}

func renderSummaryPlain(w io.Writer, operation string, success, failed, skipped int) {
	parts := []string{fmt.Sprintf("%d resources %s", success, operation)}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d errors", failed))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", skipped))
	}

	icon := "✔ "
	if failed > 0 && success == 0 {
		icon = "✘ "
	} else if failed > 0 {
		icon = "⚠ "
	}
	fmt.Fprintln(w, icon+strings.Join(parts, ", "))
}

func renderSummaryStyled(w io.Writer, operation string, success, failed, skipped int) {
	title := Gradient(strings.ToUpper(operation)+" COMPLETE", GradientBrandFrom, GradientBrandTo)

	successStyle := lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	failedStyle := lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	skippedStyle := lipgloss.NewStyle().Foreground(ColorWarning)
	mutedStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var lines []string
	lines = append(lines, successStyle.Render(fmt.Sprintf("  %d succeeded", success)))
	if failed > 0 {
		lines = append(lines, failedStyle.Render(fmt.Sprintf("  %d failed", failed)))
	}
	if skipped > 0 {
		lines = append(lines, skippedStyle.Render(fmt.Sprintf("  %d skipped", skipped)))
	}

	body := strings.Join(lines, mutedStyle.Render("  ·"))

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(0, 1)

	fmt.Fprintln(w, panel.Render(title+"\n"+body))
}
