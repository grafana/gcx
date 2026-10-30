//go:build !wasip1

package style

import "charm.land/glamour/v2"

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
