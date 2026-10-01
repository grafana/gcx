// Package terminal provides TTY detection and global pipe state for gcx.
//
// Detection happens in root PersistentPreRun by calling [Detect]. The result
// is stored as package-level state accessible via [IsPiped] and [NoTruncate].
// The process stdout/stderr files queried by [StdoutIsTerminal],
// [StderrIsTerminal] and [StdoutWidth] are captured at init and re-captured
// by [Detect], which drops them when the invocation is sandboxed.
// The [SetPiped] and [SetNoTruncate] setters allow the CLI layer to override
// the detected values (e.g., from --no-truncate flag or agent mode).
package terminal

import (
	"context"
	"os"
	"sync/atomic"

	"github.com/grafana/gcx/internal/host"
	"golang.org/x/term"
)

var (
	piped      atomic.Bool //nolint:gochecknoglobals
	noTruncate atomic.Bool //nolint:gochecknoglobals

	// stdout and stderr are the process stdio files, or nil when the
	// invocation has none (embedded in another process).
	stdout atomic.Pointer[os.File] //nolint:gochecknoglobals
	stderr atomic.Pointer[os.File] //nolint:gochecknoglobals
)

func init() { //nolint:gochecknoinits
	// Capture the process stdio so TTY checks work before Detect runs (e.g.
	// help output, tests).
	captureStdio(context.Background())
}

func captureStdio(ctx context.Context) {
	out, _ := host.StdoutFile(ctx)
	stdout.Store(out)
	errFile, _ := host.StderrFile(ctx)
	stderr.Store(errFile)
}

// Detect examines stdout to determine whether it is connected to a terminal.
// When stdout is not a TTY (i.e., piped), IsPiped is set to true and NoTruncate
// is also set to true automatically. Call this once from root PersistentPreRun.
func Detect(ctx context.Context) {
	captureStdio(ctx)
	isPiped := !StdoutIsTerminal()
	piped.Store(isPiped)
	if isPiped {
		noTruncate.Store(true)
	}
}

// StdoutIsTerminal reports whether stdout is connected to a real terminal.
func StdoutIsTerminal() bool {
	f := stdout.Load()
	return f != nil && term.IsTerminal(int(f.Fd()))
}

// StderrIsTerminal reports whether stderr is connected to a real terminal.
func StderrIsTerminal() bool {
	f := stderr.Load()
	return f != nil && term.IsTerminal(int(f.Fd()))
}

// StdoutWidth returns the current stdout terminal width, or 0 when unknown.
func StdoutWidth() int {
	f := stdout.Load()
	if f == nil {
		return 0
	}
	width, _, err := term.GetSize(int(f.Fd()))
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

// StdoutSize returns the stdout terminal size, failing when stdout is not a
// terminal.
func StdoutSize() (int, int, error) {
	f := stdout.Load()
	if f == nil {
		return 0, 0, os.ErrInvalid
	}
	return term.GetSize(int(f.Fd()))
}
