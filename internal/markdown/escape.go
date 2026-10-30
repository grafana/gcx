// Package markdown holds Markdown helpers shared by the terminal help renderer
// and the generated CLI reference.
package markdown

import (
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// EscapeRawHTML escapes the spans that Markdown parses as raw HTML, so a help
// placeholder such as <title> renders as text. Unescaped, Hugo passes it to the
// browser as an element, whose tokenizer then swallows the rest of the page,
// and glamour drops it from terminal help. Both render with goldmark, so its
// own classification decides which spans to escape; code fences, code spans,
// and autolinks are left alone.
func EscapeRawHTML(markdown string) string {
	source := []byte(markdown)
	var spans []text.Segment
	// The walker never fails, so the only error Walk can return is nil.
	_ = ast.Walk(goldmark.DefaultParser().Parse(text.NewReader(source)), func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *ast.RawHTML:
			for i := range node.Segments.Len() {
				spans = append(spans, node.Segments.At(i))
			}
		case *ast.HTMLBlock:
			for i := range node.Lines().Len() {
				spans = append(spans, node.Lines().At(i))
			}
			if node.HasClosure() {
				spans = append(spans, node.ClosureLine)
			}
		}
		return ast.WalkContinue, nil
	})
	slices.SortFunc(spans, func(a, b text.Segment) int { return a.Start - b.Start })
	escaper := strings.NewReplacer("<", "&lt;", ">", "&gt;")
	var out strings.Builder
	end := 0
	for _, span := range spans {
		if span.Start < end {
			continue
		}
		out.WriteString(markdown[end:span.Start])
		out.WriteString(escaper.Replace(markdown[span.Start:span.Stop]))
		end = span.Stop
	}
	out.WriteString(markdown[end:])
	return out.String()
}
