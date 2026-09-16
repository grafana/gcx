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
// Delimiter positions are found by scanning a quote-masked copy of expr (see
// maskQuoted) — a brace inside a double-quoted or backtick-quoted string is
// never a selector delimiter — but the returned selector text is sliced from
// the original expr, since masking preserves byte positions/length exactly.
//
// Returns nil if no selector could be found (expr has no "{...}" block).
func ExtractStreamSelectors(expr string) []string {
	masked := maskQuoted(expr)

	var selectors []string
	seen := make(map[string]bool)
	selectorStart := -1

	for i := range len(masked) {
		switch masked[i] {
		case '{':
			if selectorStart == -1 {
				selectorStart = i
			}
		case '}':
			if selectorStart != -1 {
				selector := expr[selectorStart : i+1]
				if !seen[selector] {
					seen[selector] = true
					selectors = append(selectors, selector)
				}
				selectorStart = -1
			}
		}
	}

	return selectors
}
