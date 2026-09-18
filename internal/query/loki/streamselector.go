package loki

import (
	"strings"
)

// LabelMatcher is a single label matcher from a LogQL stream selector, e.g.
// the `app="foo"` in `{app="foo"}`.
type LabelMatcher struct {
	Key      string
	Operator string // one of =, !=, =~, !~
	Value    string
}

// Inclusive reports whether this matcher is a positive match (= or =~), as
// opposed to an exclusion (!= or !~).
func (m LabelMatcher) Inclusive() bool {
	return m.Operator == "=" || m.Operator == "=~"
}

// LineFilter is a single line filter from a LogQL pipeline, e.g. the
// `|= "error"` in `{app="foo"} |= "error"`.
type LineFilter struct {
	Operator string // one of |=, !=, |~, !~
	Value    string
}

// ParseStreamSelector extracts the label matchers and simple line filters from
// a LogQL expression, for building a Logs Drilldown deep link. It only
// recognizes a bare stream selector optionally followed by simple line
// filters — anything else (parser stages, label_format, aggregation
// functions, unwrap, etc.) makes it return ok=false so callers can fall back
// to a plain Explore link instead of building an inaccurate Drilldown link.
func ParseStreamSelector(expr string) ([]LabelMatcher, []LineFilter, bool) {
	expr = strings.TrimSpace(expr)

	selector, rest, ok := splitSelector(expr)
	if !ok {
		return nil, nil, false
	}

	matchers, ok := parseMatchers(selector)
	if !ok {
		return nil, nil, false
	}

	lineFilters, ok := parseLineFilters(rest)
	if !ok {
		return nil, nil, false
	}

	return matchers, lineFilters, true
}

// splitSelector separates the leading {...} stream selector from the
// remainder of the expression.
func splitSelector(expr string) (string, string, bool) {
	if !strings.HasPrefix(expr, "{") {
		return "", "", false
	}

	depth := 0
	inQuotes := false
	escaped := false
	for i, c := range expr {
		switch {
		case inQuotes:
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inQuotes = false
			}
		case c == '"':
			inQuotes = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return expr[1:i], strings.TrimSpace(expr[i+1:]), true
			}
		}
	}

	return "", "", false
}

// parseMatchers parses the comma-separated key op "value" pairs inside a
// stream selector's braces.
func parseMatchers(selector string) ([]LabelMatcher, bool) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return nil, false
	}

	var matchers []LabelMatcher
	for _, part := range splitTopLevelCommas(selector) {
		m, ok := parseSingleMatcher(strings.TrimSpace(part))
		if !ok {
			return nil, false
		}
		matchers = append(matchers, m)
	}

	if len(matchers) == 0 {
		return nil, false
	}

	return matchers, true
}

func parseSingleMatcher(part string) (LabelMatcher, bool) {
	for _, op := range []string{"!~", "=~", "!=", "="} {
		idx := strings.Index(part, op)
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(part[:idx])
		rawValue := strings.TrimSpace(part[idx+len(op):])
		value, ok := unquote(rawValue)
		if !ok || key == "" {
			continue
		}
		return LabelMatcher{Key: key, Operator: op, Value: value}, true
	}
	return LabelMatcher{}, false
}

// parseLineFilters parses a sequence of simple `OP "value"` line filters
// (|=, !=, |~, !~) following a stream selector. Returns ok=false if rest is
// non-empty but doesn't parse as a clean sequence of these filters (e.g. it
// contains a parser stage like `| json`).
func parseLineFilters(rest string) ([]LineFilter, bool) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil, true
	}

	var filters []LineFilter
	for rest != "" {
		op, ok := matchLineFilterOperator(rest)
		if !ok {
			return nil, false
		}
		rest = strings.TrimSpace(rest[len(op):])

		value, remainder, ok := readQuotedValue(rest)
		if !ok {
			return nil, false
		}
		filters = append(filters, LineFilter{Operator: op, Value: value})
		rest = strings.TrimSpace(remainder)
	}

	return filters, true
}

func matchLineFilterOperator(s string) (string, bool) {
	for _, op := range []string{"|=", "!=", "|~", "!~"} {
		if strings.HasPrefix(s, op) {
			return op, true
		}
	}
	return "", false
}

// readQuotedValue reads a leading double-quoted string, returning its
// unquoted content and the remainder of s after the closing quote.
func readQuotedValue(s string) (string, string, bool) {
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}

	escaped := false
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			unquoted, ok := unquote(s[:i+1])
			if !ok {
				return "", "", false
			}
			return unquoted, s[i+1:], true
		}
	}

	return "", "", false
}

// unquote strips a single layer of double quotes and resolves backslash
// escapes, mirroring LogQL's quoted-string syntax. It does not accept
// backtick-quoted (raw) strings, since Drilldown's own matcher parsing
// doesn't special-case them either.
func unquote(s string) (string, bool) {
	if len(s) < 2 || !strings.HasPrefix(s, `"`) || !strings.HasSuffix(s, `"`) {
		return "", false
	}
	inner := s[1 : len(s)-1]

	var sb strings.Builder
	escaped := false
	for _, c := range inner {
		switch {
		case escaped:
			sb.WriteRune(c)
			escaped = false
		case c == '\\':
			escaped = true
		default:
			sb.WriteRune(c)
		}
	}
	if escaped {
		return "", false
	}
	return sb.String(), true
}

// splitTopLevelCommas splits s on commas that are not inside a quoted string.
func splitTopLevelCommas(s string) []string {
	var parts []string
	inQuotes := false
	escaped := false
	start := 0
	for i, c := range s {
		switch {
		case inQuotes:
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inQuotes = false
			}
		case c == '"':
			inQuotes = true
		case c == ',':
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
