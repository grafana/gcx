package output

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/grafana/gcx/internal/style"
)

//nolint:gochecknoglobals
var (
	greenStyle  = lipgloss.NewStyle().Foreground(style.ColorSuccess)
	blueStyle   = lipgloss.NewStyle().Foreground(style.ColorPrimary)
	redStyle    = lipgloss.NewStyle().Foreground(style.ColorError)
	yellowStyle = lipgloss.NewStyle().Foreground(style.ColorWarning)
	boldStyle   = lipgloss.NewStyle().Bold(true)
)

func colorSprintf(s lipgloss.Style) func(format string, a ...any) string {
	return func(format string, a ...any) string {
		text := fmt.Sprintf(format, a...)
		if !style.IsStylingEnabled() {
			return text
		}
		return s.Render(text)
	}
}

//nolint:gochecknoglobals
var (
	Green  = colorSprintf(greenStyle)
	Blue   = colorSprintf(blueStyle)
	Red    = colorSprintf(redStyle)
	Yellow = colorSprintf(yellowStyle)
	Bold   = colorSprintf(boldStyle)
)

func Success(stdout io.Writer, message string, args ...any) {
	msg := fmt.Sprintf(message, args...)

	fmt.Fprintln(stdout, Green("✔ ")+msg)
}

func Warning(stdout io.Writer, message string, args ...any) {
	msg := fmt.Sprintf(message, args...)

	fmt.Fprintln(stdout, Yellow("⚠ ")+msg)
}

func Error(stdout io.Writer, message string, args ...any) {
	msg := fmt.Sprintf(message, args...)

	fmt.Fprintln(stdout, Red("✘ ")+msg)
}

func Info(stdout io.Writer, message string, args ...any) {
	msg := fmt.Sprintf(message, args...)

	fmt.Fprintln(stdout, Blue("🛈 ")+msg)
}
