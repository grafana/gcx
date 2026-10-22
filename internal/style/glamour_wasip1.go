package style

import "errors"

// renderMarkdown is unavailable in wasip1 builds, which only run embedded
// with styling off. Leaving glamour out drops its chroma syntax highlighter,
// whose lexer and style registration dominates module start-up. Callers fall
// back to the raw markdown.
func renderMarkdown(string, bool) (string, error) {
	return "", errors.New("markdown rendering is not available in wasip1 builds")
}
