package tempo

import "strings"

// TraceQLFilter is a single scope.tag op value comparison from a flat,
// &&-joined TraceQL spanset, e.g. the `span.http.status_code = 500` in
// `{ span.http.status_code = 500 }`.
type TraceQLFilter struct {
	Scope    string // span, resource, trace, parent, instrumentation
	Tag      string // e.g. "http.status_code", "service.name"
	Operator string // one of =, !=, >, <, =~, !~
	Value    string
}

// isTraceQLScope reports whether scope is one of the attribute scopes Traces
// Drilldown's var-filters encoding recognizes with a "." separator (e.g.
// "span.http.status_code"). Bare/unscoped intrinsics (duration, status,
// kind, name, ...), which Drilldown instead separates with ":", are
// deliberately not supported here — its exact intrinsic list couldn't be
// verified from source, so filters using them fall back to a plain Explore
// link instead of risking an inaccurate one.
func isTraceQLScope(scope string) bool {
	switch scope {
	case "span", "resource", "trace", "parent", "instrumentation":
		return true
	default:
		return false
	}
}

// ParseFlatSpansetFilters extracts &&-joined scope.tag op value comparisons
// from a single, non-nested TraceQL spanset, for building a Traces Drilldown
// deep link. It only recognizes this flat form — anything else (||, nested
// spansets, pipeline/structural stages like | select(...), >>, ~>, additional
// spansets, bare/unscoped intrinsics, or operators Drilldown doesn't support
// such as >=/<=) makes it return ok=false so callers fall back to a plain
// Explore link instead of building an inaccurate Drilldown link.
func ParseFlatSpansetFilters(expr string) ([]TraceQLFilter, bool) {
	expr = strings.TrimSpace(expr)

	spanset, rest, ok := splitSpanset(expr)
	if !ok || strings.TrimSpace(rest) != "" {
		return nil, false
	}

	return parseANDedComparisons(spanset)
}

// splitSpanset separates the leading {...} spanset from the remainder of the
// expression, mirroring the brace/quote depth tracking used for LogQL stream
// selectors.
func splitSpanset(expr string) (string, string, bool) {
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

// parseANDedComparisons parses the &&-joined comparisons inside a spanset's
// braces. Any top-level || makes the whole spanset unsupported, since mixed
// or purely-OR logic has no representation in this flat filter model.
func parseANDedComparisons(spanset string) ([]TraceQLFilter, bool) {
	spanset = strings.TrimSpace(spanset)
	if spanset == "" || containsTopLevel(spanset, "||") {
		return nil, false
	}

	var filters []TraceQLFilter
	for _, part := range splitTopLevel(spanset, "&&") {
		f, ok := parseComparison(strings.TrimSpace(part))
		if !ok {
			return nil, false
		}
		filters = append(filters, f)
	}

	if len(filters) == 0 {
		return nil, false
	}

	return filters, true
}

func parseComparison(part string) (TraceQLFilter, bool) {
	// Longer operators must be checked before their single-character
	// prefixes (=~ before =, >= before >, <= before <) so e.g. ">=" isn't
	// misparsed as ">" with a value of "=500".
	for _, op := range []string{"!=", "=~", "!~", ">=", "<=", "=", ">", "<"} {
		idx := strings.Index(part, op)
		if idx <= 0 {
			continue
		}
		if op == ">=" || op == "<=" {
			// Not supported by Traces Drilldown's filter model.
			return TraceQLFilter{}, false
		}

		scope, tag, ok := splitScopedTag(strings.TrimSpace(part[:idx]))
		if !ok {
			return TraceQLFilter{}, false
		}

		value, ok := parseComparisonValue(strings.TrimSpace(part[idx+len(op):]))
		if !ok {
			return TraceQLFilter{}, false
		}

		return TraceQLFilter{Scope: scope, Tag: tag, Operator: op, Value: value}, true
	}
	return TraceQLFilter{}, false
}

// splitScopedTag splits "span.http.status_code" into ("span",
// "http.status_code"), requiring the scope to be one of the recognized
// attribute scopes. A bare/unscoped intrinsic like "duration" has no "." and
// is rejected here.
func splitScopedTag(key string) (string, string, bool) {
	dot := strings.Index(key, ".")
	if dot <= 0 || dot == len(key)-1 {
		return "", "", false
	}
	scope, tag := key[:dot], key[dot+1:]
	if !isTraceQLScope(scope) || tag == "" {
		return "", "", false
	}
	return scope, tag, true
}

// parseComparisonValue accepts a double-quoted string (unquoted and
// unescaped) or a bare token (number, duration, boolean).
func parseComparisonValue(raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, `"`) {
		return unquote(raw)
	}
	if strings.ContainsAny(raw, ` "`) {
		// A bare token shouldn't contain whitespace or quotes; this signals
		// leftover content the flat grammar doesn't understand.
		return "", false
	}
	return raw, true
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

// containsTopLevel reports whether sub appears in s outside of any
// double-quoted string.
func containsTopLevel(s, sub string) bool {
	inQuotes := false
	escaped := false
	for i := range len(s) {
		c := s[i]
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
		case strings.HasPrefix(s[i:], sub):
			return true
		}
	}
	return false
}

// splitTopLevel splits s on occurrences of sep that are not inside a quoted
// string.
func splitTopLevel(s, sep string) []string {
	var parts []string
	inQuotes := false
	escaped := false
	start := 0
	i := 0
	for i < len(s) {
		c := s[i]
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
			i++
		case c == '"':
			inQuotes = true
			i++
		case strings.HasPrefix(s[i:], sep):
			parts = append(parts, s[start:i])
			i += len(sep)
			start = i
		default:
			i++
		}
	}
	parts = append(parts, s[start:])
	return parts
}
