package loki

import (
	"fmt"
	"strconv"
	"strings"
)

// logsDrilldownPluginID is the Grafana app plugin ID for Logs Drilldown.
const logsDrilldownPluginID = "grafana-lokiexplore-app"

// escapeURLDelimiters mirrors Logs Drilldown's own escapeURLDelimiters: it
// replaces the comma and pipe characters used as value/filter separators in
// its flat var-* query params, so a label or line-filter value containing
// either doesn't corrupt the encoding.
func escapeURLDelimiters(value string) string {
	value = strings.ReplaceAll(value, ",", "__gfc__")
	value = strings.ReplaceAll(value, "|", "__gfp__")
	return value
}

// userInputAdHocValuePrefix mirrors Logs Drilldown's USER_INPUT_ADHOC_VALUE_PREFIX,
// the marker it uses to denote a value that came from raw user/query input
// (as opposed to one picked from an ad-hoc filter's known value list) so it
// isn't re-escaped when Drilldown renders the LogQL back out.
const userInputAdHocValuePrefix = "__CVΩ__"

// escapePrimaryLabel mirrors Logs Drilldown's escapePrimaryLabel: it
// normalizes path-breaking characters in the primary label value used as a
// URL path segment, then percent-encodes the result the same way
// JavaScript's encodeURIComponent does (which the real app uses) —
// notably escaping a space as %20, not Go url.QueryEscape's query-string
// "+".
func escapePrimaryLabel(value string) string {
	normalized := strings.ReplaceAll(value, "/", "-")
	normalized = strings.ReplaceAll(normalized, "\\", "-")
	return encodeURIComponent(normalized)
}

// encodeURIComponent percent-encodes s the same way JavaScript's
// encodeURIComponent does: every byte except unreserved
// (A-Z a-z 0-9 - _ . ! ~ * ' ( )) is escaped, byte-for-byte (which also
// correctly percent-encodes each byte of a multi-byte UTF-8 rune, matching
// encodeURIComponent's own per-UTF-8-byte escaping of non-ASCII input).
func encodeURIComponent(s string) string {
	var sb strings.Builder
	for i := range len(s) {
		c := s[i]
		if isURIComponentSafe(c) {
			sb.WriteByte(c)
		} else {
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}

func isURIComponentSafe(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	}
	return false
}

// emptyAdHocValueSentinel mirrors Logs Drilldown's EMPTY_VARIABLE_VALUE
// (src/services/variables.ts): the literal two-character string
// stringifyAdHocValues emits for an empty value, instead of the ad-hoc
// user-input prefix applied to an empty string.
const emptyAdHocValueSentinel = `""`

// encodeLabelFilter renders one var-filters entry, mirroring Logs Drilldown's
// setUrlParamsFromLabelFilters dual-value encoding: an ad-hoc-prefixed,
// delimiter-escaped copy of the value followed by a plain delimiter-escaped
// copy, comma-separated. An empty value uses Drilldown's own empty-value
// sentinel for the ad-hoc half instead of the ad-hoc prefix applied to "".
func encodeLabelFilter(key, operator, value string) string {
	adhoc := emptyAdHocValueSentinel
	if value != "" {
		adhoc = escapeURLDelimiters(userInputAdHocValuePrefix + value)
	}
	plain := escapeURLDelimiters(value)
	return key + "|" + operator + "|" + adhoc + "," + plain
}

// encodeLineFilter renders one var-lineFilters entry, mirroring Logs
// Drilldown's setLineFilterUrlParams encoding: key, then the operator and
// value, each delimiter-escaped (the operator needs escaping too, since |=
// and |~ contain the pipe delimiter character itself).
func encodeLineFilter(key, operator, value string) string {
	return key + "|" + escapeURLDelimiters(operator) + "|" + escapeURLDelimiters(value)
}

// isRegexLineFilterOperator reports whether op is one of LogQL's regex line
// filter operators (|~, !~), as opposed to the literal substring ones
// (|=, !=).
func isRegexLineFilterOperator(op string) bool {
	return op == "|~" || op == "!~"
}

// Line-filter var-lineFilters key components, mirroring Logs Drilldown's
// LineFilterCaseSensitive enum (src/services/filterTypes.ts).
const (
	caseSensitiveLineFilterKey   = "caseSensitive"
	caseInsensitiveLineFilterKey = "caseInsensitive"
)

// lineFilterKeyAndValue computes the var-lineFilters key and value for the
// line filter at position index among all of a query's line filters,
// mirroring Logs Drilldown's parseNonPatternFilters: a regex filter
// (|~/!~) whose value contains the "(?i)" case-insensitivity marker uses
// the literal key "caseInsensitive" with the marker stripped from the
// value; every other filter uses "caseSensitive,<index>" with the value
// unchanged.
func lineFilterKeyAndValue(index int, operator, value string) (string, string) {
	if isRegexLineFilterOperator(operator) && strings.Contains(value, "(?i)") {
		return caseInsensitiveLineFilterKey, strings.Replace(value, "(?i)", "", 1)
	}
	return caseSensitiveLineFilterKey + "," + strconv.Itoa(index), value
}
