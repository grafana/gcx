package query

import (
	"net/url"
	"strings"
)

// LogsDrilldownPluginID is the Grafana app plugin ID for Logs Drilldown.
const LogsDrilldownPluginID = "grafana-lokiexplore-app"

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

// EscapePrimaryLabel mirrors Logs Drilldown's escapePrimaryLabel: it
// normalizes path-breaking characters in the primary label value used as a
// URL path segment, then percent-encodes the result.
func EscapePrimaryLabel(value string) string {
	normalized := strings.ReplaceAll(value, "\\", "-")
	normalized = strings.ReplaceAll(normalized, "/", "-")
	return url.QueryEscape(normalized)
}

// EncodeLabelFilter renders one var-filters entry, mirroring Logs Drilldown's
// setUrlParamsFromLabelFilters dual-value encoding: an ad-hoc-prefixed,
// delimiter-escaped copy of the value followed by a plain delimiter-escaped
// copy, comma-separated.
func EncodeLabelFilter(key, operator, value string) string {
	adhoc := escapeURLDelimiters(userInputAdHocValuePrefix + value)
	plain := escapeURLDelimiters(value)
	return key + "|" + operator + "|" + adhoc + "," + plain
}

// EncodeLineFilter renders one var-lineFilters entry, mirroring Logs
// Drilldown's setLineFilterUrlParams encoding: key, then the operator and
// value, each delimiter-escaped (the operator needs escaping too, since |=
// and |~ contain the pipe delimiter character itself).
func EncodeLineFilter(key, operator, value string) string {
	return key + "|" + escapeURLDelimiters(operator) + "|" + escapeURLDelimiters(value)
}

// BuildDrilldownURL renders a Grafana Drilldown app URL for pluginID (e.g.
// LogsDrilldownPluginID). path is the app-relative path (e.g.
// "/explore/service/foo/logs" or "/explore" for the root), and params holds
// the flat, possibly-repeated query parameters (var-filters, var-lineFilters,
// etc.) already in that app's encoded form.
func BuildDrilldownURL(host, pluginID, path string, params map[string][]string) string {
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return ""
	}
	if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
		return ""
	}

	u, err := url.Parse(host + "/a/" + pluginID + path)
	if err != nil {
		return ""
	}

	query := u.Query()
	for key, values := range params {
		for _, v := range values {
			query.Add(key, v)
		}
	}
	u.RawQuery = query.Encode()

	return u.String()
}
