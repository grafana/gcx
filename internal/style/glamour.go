//go:build !wasip1

package style

import (
	"charm.land/glamour/v2"
	"github.com/grafana/gcx/internal/markdown"
)

// renderMarkdown styles md for a dark terminal. With wrap false, word wrap
// is disabled so long tokens such as URLs stay on one logical line.
func renderMarkdown(md string, wrap bool) (string, error) {
	opts := []glamour.TermRendererOption{glamour.WithStandardStyle("dark")}
	if !wrap {
		opts = append(opts, glamour.WithWordWrap(0))
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return "", err
	}
	return r.Render(md)
}

// renderLong renders a command's Long description with word wrap disabled so
// long tokens such as documentation URLs stay on a single logical line and
// remain clickable in terminals that auto-detect links. Raw HTML is escaped
// because glamour drops it, and with it placeholders such as <name>.
func renderLong(long string) (string, error) {
	return renderMarkdown(markdown.EscapeRawHTML(long), false)
}
