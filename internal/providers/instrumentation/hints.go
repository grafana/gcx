package instrumentation

import (
	"fmt"
	"io"
	"sync"
)

// Provider-local diagnostic footer. Candidate for promotion to a shared
// CLI-wide mechanism once cross-cutting conventions are settled — tracked
// in grafana/gcx#549.
//
// Footer accumulates structured diagnostic output during command execution
// and flushes it to stderr AFTER the command's primary (stdout) result.
// It enforces the rendering order:
//
//  1. warn: <single-line message>    — per-item failures
//  2. note: <single-line message>    — caveats, state observations, footers
//  3. hint: to <purpose>:            — next-step suggestions
//     <command>                      (indented, copy-paste friendly)
//
// Layout rules (enforced by Print):
//   - A single leading blank line separates the footer from preceding
//     output. Not emitted when the footer is empty.
//   - Warns and notes are one line each, no inter-item blank.
//   - Hints are two-line blocks. Successive hints are separated by a
//     blank line.
//   - No trailing blank line.
//
// Footer is safe for concurrent use so aggregate commands (fan-out via
// errgroup) can accumulate warns from worker goroutines.
//
// Usage:
//
//	var f instrumentation.Footer
//	if err := opts.IO.Encode(cmd.OutOrStdout(), results); err != nil {
//	    return err
//	}
//	f.Notef("data reflects Alloy collector-reported state (refresh ~30s).")
//	f.Print(cmd.ErrOrStderr())
//	return nil
//
// Cap at 3 hints per site. Past 3, write documentation instead.
type Footer struct {
	mu    sync.Mutex
	warns []string
	notes []string
	hints []hintEntry
}

type hintEntry struct {
	purpose string
	command string
}

// Warnf records a per-item failure warning.
func (f *Footer) Warnf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warns = append(f.warns, fmt.Sprintf(format, args...))
}

// Notef records a caveat or state observation.
func (f *Footer) Notef(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notes = append(f.notes, fmt.Sprintf(format, args...))
}

// Hint records a next-step suggestion. The command lives on its own
// indented line in the rendered output so terminal select-line /
// triple-click yields a pasteable string.
func (f *Footer) Hint(purpose, command string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hints = append(f.hints, hintEntry{purpose: purpose, command: command})
}

// IsEmpty reports whether the footer has any recorded items.
func (f *Footer) IsEmpty() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.warns) == 0 && len(f.notes) == 0 && len(f.hints) == 0
}

// Print flushes the accumulated items to w in warn → note → hint order.
// No-op when the footer is empty.
func (f *Footer) Print(w io.Writer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.warns) == 0 && len(f.notes) == 0 && len(f.hints) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, s := range f.warns {
		fmt.Fprintf(w, "warn: %s\n", s)
	}
	for _, s := range f.notes {
		fmt.Fprintf(w, "note: %s\n", s)
	}
	for i, h := range f.hints {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "hint: %s:\n  %s\n", h.purpose, h.command)
	}
}
