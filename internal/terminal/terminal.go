// Package terminal provides TTY detection and global pipe state for gcx.
//
// Detection happens in root PersistentPreRun by calling [Detect]. The result
// is stored as package-level state accessible via [IsPiped] and [NoTruncate].
// The [SetPiped] and [SetNoTruncate] setters allow the CLI layer to override
// the detected values (e.g., from --no-truncate flag or agent mode).
package terminal

import (
	"os"
	"sync/atomic"

	"golang.org/x/term"
)

var (
	piped      atomic.Bool //nolint:gochecknoglobals
	noTruncate atomic.Bool //nolint:gochecknoglobals
)

// Detect examines os.Stdout to determine whether it is connected to a terminal.
// When stdout is not a TTY (i.e., piped), IsPiped is set to true and NoTruncate
// is also set to true automatically. Call this once from root PersistentPreRun.
func Detect() {
	isPiped := !StdoutIsTerminal()
	piped.Store(isPiped)
	if isPiped {
		noTruncate.Store(true)
	}
}

// StdoutIsTerminal reports whether stdout is connected to a real terminal.
func StdoutIsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// StdinIsTerminal reports whether stdin is connected to a real terminal.
func StdinIsTerminal() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// StdoutWidth returns the current stdout terminal width, or 0 when unknown.
func StdoutWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 0
	}
	return width
}

// IsPiped reports whether stdout is not connected to a terminal.
func IsPiped() bool {
	return piped.Load()
}

// SetPiped overrides the detected pipe state. Used by the CLI layer when agent
// mode is active (which implies piped behavior regardless of actual TTY state).
func SetPiped(v bool) {
	piped.Store(v)
}

// NoTruncate reports whether table column truncation should be suppressed.
// This is true when stdout is piped (auto-detected) or when --no-truncate is
// explicitly passed.
func NoTruncate() bool {
	return noTruncate.Load()
}

// SetNoTruncate overrides the no-truncate state. Used by the CLI layer when
// --no-truncate is passed or when agent mode implies no-truncate behavior.
func SetNoTruncate(v bool) {
	noTruncate.Store(v)
}

// ResetForTesting resets all package-level state to zero values.
// Exported for use in tests only.
func ResetForTesting() {
	piped.Store(false)
	noTruncate.Store(false)
}
