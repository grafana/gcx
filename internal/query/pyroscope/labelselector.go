package pyroscope

import "strings"

// LabelMatcher is a single label matcher from a Pyroscope label selector,
// e.g. the `service_name="frontend"` in `{service_name="frontend"}`.
type LabelMatcher struct {
	Key      string
	Operator string // one of =, !=, =~, !~
	Value    string
}

// ParseLabelSelector extracts the comma-separated label matchers from a
// Pyroscope label selector (`{key="value", key2!="value2"}`), for building a
// Profiles Drilldown deep link. Returns ok=false if selector isn't a single,
// well-formed `{...}` block of matchers.
func ParseLabelSelector(selector string) ([]LabelMatcher, bool) {
	selector = strings.TrimSpace(selector)
	if !strings.HasPrefix(selector, "{") || !strings.HasSuffix(selector, "}") {
		return nil, false
	}
	inner := strings.TrimSpace(selector[1 : len(selector)-1])
	if inner == "" {
		return nil, true
	}

	var matchers []LabelMatcher
	for _, part := range splitTopLevelCommas(inner) {
		m, ok := parseSingleMatcher(strings.TrimSpace(part))
		if !ok {
			return nil, false
		}
		matchers = append(matchers, m)
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

// unquote strips a single layer of double quotes and resolves backslash
// escapes.
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
