package loki

// ExtractStreamSelectors returns every LogQL stream selector (each leading
// "{...}" matcher block) found in a LogQL expression, in the order they
// appear, deduplicated by exact text. It strips any pipeline stages,
// range/aggregation wrapping (e.g. "rate(...)[5m]"), or other syntax around
// each selector.
//
// Loki's index/stats endpoint only accepts a single bare selector — it
// rejects a full expression with "only label matchers are supported" —
// while query/metrics/stats all accept a full expression from the user,
// which may combine multiple selectors via a binary operator (e.g.
// "count_over_time({a}[5m]) + count_over_time({b}[5m])"). Callers estimate
// total cost by calling index/stats once per selector returned here and
// summing.
//
// Quote state is tracked in one pass across the whole expression, not reset
// per candidate "{" — otherwise a brace inside a quoted string in a later
// pipeline stage (e.g. `{app="x"} |= "payload {foo}"`) would be misread as a
// second selector once scanning resumes past the real one.
//
// Returns nil if no selector could be found (expr has no "{...}" block).
func ExtractStreamSelectors(expr string) []string {
	var selectors []string
	seen := make(map[string]bool)

	inQuotes := false
	escaped := false
	selectorStart := -1

	for i := range len(expr) {
		c := expr[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			inQuotes = !inQuotes
		case inQuotes:
			// Braces (and everything else) inside a quoted string are never
			// selector delimiters, regardless of whether we're currently
			// inside an open "{...}" or not.
		case c == '{' && selectorStart == -1:
			selectorStart = i
		case c == '}' && selectorStart != -1:
			selector := expr[selectorStart : i+1]
			if !seen[selector] {
				seen[selector] = true
				selectors = append(selectors, selector)
			}
			selectorStart = -1
		}
	}

	return selectors
}
