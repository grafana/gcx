package loki

import "sort"

// summarizePatterns prepares freshly decoded patterns for output in place.
func summarizePatterns(patterns []Pattern) []Pattern {
	for i := range patterns {
		for _, sample := range patterns[i].Samples {
			if len(sample) >= 2 {
				patterns[i].TotalCount += sample[1]
			}
		}
	}
	sort.Slice(patterns, func(i, j int) bool {
		if patterns[i].TotalCount != patterns[j].TotalCount {
			return patterns[i].TotalCount > patterns[j].TotalCount
		}
		// Break ties by text so backend ordering cannot make identical results awkward to diff.
		return patterns[i].Pattern < patterns[j].Pattern
	})
	return patterns
}
