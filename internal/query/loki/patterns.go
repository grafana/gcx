package loki

import (
	"slices"
	"sort"
)

// SummarizePatterns returns a copy with total counts computed from samples,
// sorted by total descending and then by pattern text. Samples are unchanged.
func SummarizePatterns(patterns []Pattern) []Pattern {
	patterns = slices.Clone(patterns)
	for i := range patterns {
		patterns[i].TotalCount = 0
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
