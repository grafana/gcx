package style

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
)

//nolint:gochecknoglobals
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// RunWithSpinner runs fn while displaying a spinner on w (typically stderr).
// When styling is disabled, fn runs without any visual feedback.
func RunWithSpinner(w io.Writer, title string, fn func() error) error {
	if !IsStylingEnabled() {
		return fn()
	}

	spinStyle := lipgloss.NewStyle().Foreground(GradientBrandFrom)
	titleStyle := lipgloss.NewStyle().Foreground(ColorMuted)

	var done atomic.Bool
	errCh := make(chan error, 1)

	go func() {
		errCh <- fn()
		done.Store(true)
	}()

	frame := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for !done.Load() {
		fmt.Fprintf(w, "\r%s %s", spinStyle.Render(spinnerFrames[frame%len(spinnerFrames)]), titleStyle.Render(title))
		frame++
		<-ticker.C
	}

	// Clear the spinner line.
	fmt.Fprintf(w, "\r\033[K")

	return <-errCh
}
