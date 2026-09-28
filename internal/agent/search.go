package agent

import (
	"cmp"
	"math"
	"slices"
	"strings"
)

// SearchDocument separates primary routing evidence from supporting help text.
type SearchDocument struct {
	Path, Aliases, Summary, Terms string
	Description, Context          string
	// Key distinguishes workflow guides sharing the same canonical command.
	// Commands leave it empty and are identified by Path.
	Key      string
	Workflow bool
}

type searchIndex struct {
	primary map[string]int
	support []string
	path    []string
}

type searchMatch struct {
	key                      string
	exact                    bool
	workflow                 bool
	coverage, score, support float64
}

// SearchCommands returns document keys (or paths) in deterministic relevance
// order. Eligibility measures lexical evidence, not confidence or permissions.
func SearchCommands(documents []SearchDocument, query string) []string {
	terms, excluded := searchQuery(query)
	if len(terms) == 0 {
		return []string{}
	}
	indexed := make([]searchIndex, len(documents))
	frequency := make(map[string]int)
	for i, doc := range documents {
		idx := searchIndex{primary: make(map[string]int), path: searchTokens(doc.Path), support: searchTokens(doc.Description + " " + doc.Context)}
		pathTerms := doc.Path + " " + doc.Aliases
		if doc.Workflow {
			// The shared guide-reader command is an invocation, not evidence
			// that this particular workflow matches a task.
			pathTerms = ""
		}
		for _, f := range []struct {
			text   string
			weight int
		}{{pathTerms, 8}, {doc.Summary, 5}, {doc.Terms, 5}} {
			for _, term := range searchTokens(f.text) {
				idx.primary[term] = max(idx.primary[term], f.weight)
			}
		}
		for term := range idx.primary {
			frequency[term]++
		}
		indexed[i] = idx
	}
	terms = correctSearchTerms(terms, frequency)
	idf := func(term string) float64 { return 1 + math.Log(float64(len(documents)+1)/float64(frequency[term]+1)) }
	total := 0.0
	for _, term := range terms {
		total += idf(term)
	}
	matches := make([]searchMatch, 0, len(documents))
	for i, doc := range documents {
		idx := indexed[i]
		if !doc.Workflow && searchReadRequest(terms) && searchMutation(doc.Path) {
			continue
		}
		if slices.ContainsFunc(excluded, func(term string) bool { return slices.Contains(idx.path, term) }) {
			continue
		}
		key := doc.Key
		if key == "" {
			key = doc.Path
		}
		m := searchMatch{key: key, workflow: doc.Workflow, exact: !doc.Workflow && len(terms) == len(idx.path)}
		anchor := false
		for _, term := range terms {
			m.exact = m.exact && slices.Contains(idx.path, term)
			best := 0.0
			for word, weight := range idx.primary {
				quality := 0.0
				if word == term {
					quality = 1
				} else if prefixMatch(term, word) {
					quality = .7
				}
				best = max(best, quality*float64(weight))
				anchor = anchor || (quality > 0 && !searchAction(word))
			}
			if best > 0 {
				m.coverage += idf(term)
				m.score += best * idf(term)
			}
			if slices.Contains(idx.support, term) {
				m.support += idf(term)
			}
		}
		// Prefer focused command paths over descendants sharing generic words.
		m.score -= .1 * float64(len(idx.path))
		m.coverage /= total
		if m.exact || (anchor && m.coverage >= .6) {
			matches = append(matches, m)
		}
	}
	slices.SortFunc(matches, compareSearchMatches)
	paths := make([]string, 0, len(matches))
	for _, m := range matches {
		paths = append(paths, m.key)
	}
	return paths
}

func correctSearchTerms(terms []string, vocabulary map[string]int) []string {
	result := make([]string, 0, len(terms))
	for _, term := range terms {
		if vocabulary[term] == 0 && len([]rune(term)) >= 4 {
			candidate := ""
			count := 0
			hasPrefix := false
			for word := range vocabulary {
				hasPrefix = hasPrefix || prefixMatch(term, word)
				if oneEditApart(term, word) || oneEditApart(term, word+"s") {
					candidate = word
					count++
				}
			}
			if !hasPrefix && count == 1 {
				term = candidate
			}
		}
		if !slices.Contains(result, term) {
			result = append(result, term)
		}
	}
	return result
}

func prefixMatch(query, word string) bool {
	return len([]rune(query)) >= 3 && strings.HasPrefix(word, query)
}

// oneEditApart includes an adjacent transposition, but never more than one edit.
func oneEditApart(a, b string) bool {
	x, y := []rune(a), []rune(b)
	if len(x) > len(y) {
		x, y = y, x
	}
	if len(y)-len(x) > 1 {
		return false
	}
	i := 0
	for i < len(x) && x[i] == y[i] {
		i++
	}
	if i == len(x) {
		return len(y) == len(x)+1
	}
	if len(x) == len(y) {
		if slices.Equal(x[i+1:], y[i+1:]) {
			return true
		}
		return i+1 < len(x) && x[i] == y[i+1] && x[i+1] == y[i] && slices.Equal(x[i+2:], y[i+2:])
	}
	return slices.Equal(x[i:], y[i+1:])
}

func compareSearchMatches(a, b searchMatch) int {
	if a.exact != b.exact {
		if a.exact {
			return -1
		}
		return 1
	}
	for _, pair := range [][2]float64{{a.coverage, b.coverage}, {a.score, b.score}, {a.support, b.support}} {
		if n := cmp.Compare(pair[1], pair[0]); n != 0 {
			return n
		}
	}
	if a.workflow != b.workflow {
		if a.workflow {
			return 1
		}
		return -1
	}
	return strings.Compare(a.key, b.key)
}
