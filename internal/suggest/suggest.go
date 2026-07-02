// Package suggest provides fuzzy matching of a mistyped token against a
// vocabulary of valid tokens, used to build "did you mean" suggestions for
// unknown commands and flags.
package suggest

import (
	"sort"
	"strings"

	"github.com/agnivade/levenshtein"
)

// maxCandidates caps the number of returned matches so error output stays
// scannable for humans and token-cheap for agents.
const maxCandidates = 3

// Candidates returns the vocabulary entries closest to input, best match
// first. A candidate matches when its case-insensitive Levenshtein distance
// is within a length-scaled threshold, or when it starts with the input.
// Results are deduplicated and capped at three.
func Candidates(input string, vocabulary []string) []string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return nil
	}

	maxDistance := 2
	if len(input) > 8 {
		maxDistance = 3
	}

	type scored struct {
		token    string
		distance int
		order    int
	}

	seen := map[string]bool{}
	matches := []scored{}
	for i, candidate := range vocabulary {
		lower := strings.ToLower(candidate)
		if lower == "" || seen[lower] {
			continue
		}

		distance := levenshtein.ComputeDistance(input, lower)
		prefixed := len(input) >= 2 && strings.HasPrefix(lower, input)
		if distance > maxDistance && !prefixed {
			continue
		}

		seen[lower] = true
		matches = append(matches, scored{token: candidate, distance: distance, order: i})
	}

	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].distance != matches[j].distance {
			return matches[i].distance < matches[j].distance
		}
		return matches[i].order < matches[j].order
	})

	if len(matches) == 0 {
		return nil
	}
	if len(matches) > maxCandidates {
		matches = matches[:maxCandidates]
	}

	result := make([]string, 0, len(matches))
	for _, m := range matches {
		result = append(result, m.token)
	}
	return result
}
