package pyroscope

import "regexp"

// dotNoiseRe matches purely visual DOT attributes (font sizes, node ids,
// tooltips, shapes, colors) that carry no profiling information. Stripping
// them roughly halves the payload while keeping function names, file:line
// locations, self/cumulative values, and caller-callee edges intact — the
// same cleanup the Grafana Assistant applies before LLM analysis.
// Each quoted value is bounded to its own attribute with [^"]* so a value
// missing the expected terminator can never swallow neighboring attributes;
// color values are matched by attribute rather than by hex shape so named
// and short-hex colors are stripped too.
var dotNoiseRe = regexp.MustCompile(`(fontsize=\d+ )|(id="node\d+" )|(labeltooltip="[^"]*" )|(tooltip="[^"]*" )|(shape=box )|(fillcolor="[^"]*")|(color="[^"]*" )`)

// CleanDot strips visual-only attributes from a DOT profile call graph.
func CleanDot(dot string) string {
	return dotNoiseRe.ReplaceAllString(dot, "")
}

// dotNodeRe matches function node definitions (N<digit> [...]). Pyroscope
// returns a syntactically valid digraph even when the query matched no
// samples; the presence of numbered nodes is what distinguishes data from
// an empty shell.
var dotNodeRe = regexp.MustCompile(`\bN\d+\s*\[`)

// DotHasNodes reports whether a DOT profile graph contains function nodes.
func DotHasNodes(dot string) bool {
	return dotNodeRe.MatchString(dot)
}
