package agent

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// SearchDocument is the searchable metadata of one runnable CLI command.
type SearchDocument struct {
	Path        string
	Aliases     string
	Summary     string
	Description string
	Context     string
}

type searchTerm struct {
	text   string
	weight int
}

type searchMatch struct {
	path     string
	exact    bool
	coverage int
	score    int
}

// SearchCommands returns canonical paths in relevance order. Scores are lexical
// ranking signals, not confidence that a command can perform the requested task.
func SearchCommands(documents []SearchDocument, query string) []string {
	terms := searchTokens(query)
	indexed := make([][]searchTerm, len(documents))
	for i, doc := range documents {
		for _, field := range []struct {
			text   string
			weight int
		}{{doc.Path + " " + doc.Aliases, 8}, {doc.Summary, 5}, {doc.Description, 2}, {doc.Context, 1}} {
			for _, token := range searchTokens(field.text) {
				indexed[i] = append(indexed[i], searchTerm{token, field.weight})
			}
		}
	}

	// Typo matching is a fallback across the entire vocabulary: a correctly
	// spelled query must not pick up unrelated near-neighbour words.
	fuzzy := make(map[string]bool, len(terms))
	for _, term := range terms {
		fuzzy[term] = len([]rune(term)) >= 5
		for _, tokens := range indexed {
			for _, token := range tokens {
				if token.text == term || prefixMatch(term, token.text) {
					fuzzy[term] = false
				}
			}
		}
	}

	matches := make([]searchMatch, 0, len(documents))
	for i, doc := range documents {
		// A complete path wins even when phrased verb-first ("list
		// datasources"), ahead of longer paths sharing the same words.
		pathTerms := searchTokens(doc.Path)
		exact := len(terms) == len(pathTerms) && len(terms) > 0
		for _, term := range terms {
			exact = exact && slices.Contains(pathTerms, term)
		}
		match := searchMatch{path: doc.Path, exact: exact}
		for _, term := range terms {
			best := 0
			for _, token := range indexed[i] {
				quality := 0
				switch {
				case token.text == term:
					quality = 10
				case prefixMatch(term, token.text):
					quality = 7
				case fuzzy[term] && oneEditApart(term, token.text):
					quality = 4
				}
				best = max(best, quality*token.weight)
			}
			if best > 0 {
				match.coverage++
				match.score += best
			}
		}
		if match.coverage > 0 {
			matches = append(matches, match)
		}
	}
	slices.SortFunc(matches, func(a, b searchMatch) int {
		if a.exact != b.exact {
			if a.exact {
				return -1
			}
			return 1
		}
		if n := cmp.Compare(b.coverage, a.coverage); n != 0 {
			return n
		}
		if n := cmp.Compare(b.score, a.score); n != 0 {
			return n
		}
		return strings.Compare(a.path, b.path)
	})
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		paths = append(paths, match.path)
	}
	return paths
}

func searchTokens(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	result := make([]string, 0, len(words))
	for _, word := range words {
		switch word {
		case "a", "an", "and", "are", "can", "do", "for", "from", "gcx", "how", "i", "in", "is", "it", "me", "my", "of", "on", "please", "the", "to", "want", "what", "with":
			continue
		}
		if !slices.Contains(result, word) {
			result = append(result, word)
		}
	}
	return result
}

func prefixMatch(query, word string) bool {
	return len([]rune(query)) >= 3 && strings.HasPrefix(word, query)
}

// oneEditApart checks one insertion, deletion or substitution in linear time.
func oneEditApart(a, b string) bool {
	x, y := []rune(a), []rune(b)
	if len(x) > len(y) {
		x, y = y, x
	}
	if len(y)-len(x) > 1 {
		return false
	}
	i, j, edits := 0, 0, 0
	for i < len(x) && j < len(y) {
		if x[i] == y[j] {
			i++
			j++
			continue
		}
		edits++
		if edits > 1 {
			return false
		}
		if len(x) == len(y) {
			i++
		}
		j++
	}
	return edits+len(y)-j == 1
}
