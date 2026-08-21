package style

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// RenderMarkdown writes source markdown to w. When w is a terminal AND
// styling is enabled (--no-color / NO_COLOR off), the output is styled via
// glamour with word wrap disabled; otherwise the raw markdown is written
// verbatim so piping, no-color mode, and tests stay clean.
//
// Word wrap is disabled (WithWordWrap(0)) so shell commands, env-var
// examples, and URLs stay on one logical line for click-through and
// copy/paste.
//
// If glamour construction or rendering fails, the function falls through
// to writing the raw markdown so the user is never left with an empty
// output.
func RenderMarkdown(w io.Writer, source string) error {
	if IsTerminalWriter(w) && IsStylingEnabled() {
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(0))
		if err == nil {
			if out, err := r.Render(source); err == nil {
				_, err := fmt.Fprint(w, out)
				return err
			}
		}
	}
	_, err := fmt.Fprint(w, source)
	return err
}

// IsTerminalWriter reports whether w is an *os.File attached to a
// terminal. Buffers, pipes, and non-file writers are treated as
// non-terminal so raw output flows through cleanly.
func IsTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
