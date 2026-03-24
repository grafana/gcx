package output

import (
	"fmt"
	"io"

	"github.com/fatih/color"
)

//nolint:gochecknoglobals
var (
	Green  = color.New(color.FgGreen).SprintfFunc()
	Blue   = color.New(color.FgBlue).SprintfFunc()
	Red    = color.New(color.FgRed).SprintfFunc()
	Yellow = color.New(color.FgYellow).SprintfFunc()

	Bold = color.New(color.Bold).SprintfFunc()
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
