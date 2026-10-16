// Package scenesfiltertest decodes Scenes ad-hoc filter URL values for round-trip tests.
package scenesfiltertest

import "strings"

// Filter is one decoded ad-hoc filter.
type Filter struct {
	Key      string
	Operator string
	Value    string
	Origin   string
}

// Decode ports AdHocFiltersVariableUrlSyncHandler's toFilter (Scenes 7.x/8.x).
// Each part splits on "," into value and label, then unescapes.
func Decode(urlValue string) Filter {
	segments := strings.Split(urlValue, "#")
	var origin string
	if len(segments) > 1 {
		origin = segments[1]
	}

	var flat []string
	for part := range strings.SplitSeq(segments[0], "|") {
		value, label, hasLabel := strings.Cut(part, ",")
		if !hasLabel {
			label = value
		}
		flat = append(flat, unescape(value), unescape(label))
	}
	for len(flat) < 6 {
		flat = append(flat, "")
	}

	return Filter{Key: flat[0], Operator: flat[2], Value: flat[4], Origin: origin}
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, "__gfp__", "|")
	s = strings.ReplaceAll(s, "__gfc__", ",")
	return strings.ReplaceAll(s, "__gfh__", "#")
}
