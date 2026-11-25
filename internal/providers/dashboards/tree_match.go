package dashboards

import (
	"cmp"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// globMatcher matches titles against tree(1)-style wildcard patterns:
// `*` matches any run of characters (including `/`, which is common in
// dashboard titles), `?` matches one character, `[...]` matches a character
// class (`[!...]` or `[^...]` negates it), and `|` separates alternatives.
// A pattern must match the whole title.
type globMatcher struct {
	re *regexp.Regexp
}

// newGlobMatcher compiles pattern. Callers leave the matcher nil when no
// pattern was given; a nil matcher matches nothing.
func newGlobMatcher(pattern string, ignoreCase bool) (*globMatcher, error) {
	var b strings.Builder
	if ignoreCase {
		b.WriteString("(?i)")
	}
	b.WriteString("^(?:")
	for i, alt := range strings.Split(pattern, "|") {
		if i > 0 {
			b.WriteByte('|')
		}
		if err := writeGlobRegexp(&b, alt); err != nil {
			return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}
	}
	b.WriteString(")$")

	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}
	return &globMatcher{re: re}, nil
}

func writeGlobRegexp(b *strings.Builder, glob string) error {
	runes := []rune(glob)
	for i := 0; i < len(runes); i++ {
		switch r := runes[i]; r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		case '\\':
			if i+1 < len(runes) {
				i++
			}
			b.WriteString(regexp.QuoteMeta(string(runes[i])))
		case '[':
			end := i + 1
			if end < len(runes) && (runes[end] == '!' || runes[end] == '^') {
				end++
			}
			if end < len(runes) && runes[end] == ']' {
				end++
			}
			for end < len(runes) && runes[end] != ']' {
				end++
			}
			if end >= len(runes) {
				return fmt.Errorf("unterminated character class at offset %d", i)
			}
			class := runes[i+1 : end]
			b.WriteByte('[')
			if len(class) > 0 && (class[0] == '!' || class[0] == '^') {
				b.WriteByte('^')
				class = class[1:]
			}
			for _, c := range class {
				if c == '\\' || c == '[' || c == ']' {
					b.WriteByte('\\')
				}
				b.WriteRune(c)
			}
			b.WriteByte(']')
			i = end
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return nil
}

// matches reports whether title matches. A nil matcher matches nothing.
func (m *globMatcher) matches(title string) bool {
	return m != nil && m.re.MatchString(title)
}

// naturalCompare compares strings so that runs of digits are ordered by
// numeric value: "Usage 2" sorts before "Usage 10". This is the order of
// tree -v (version sort).
func naturalCompare(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return cmp.Compare(len(na), len(nb))
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		if ra[i] != rb[j] {
			return cmp.Compare(int(ra[i]), int(rb[j]))
		}
		i++
		j++
	}
	return cmp.Compare(len(ra)-i, len(rb)-j)
}
