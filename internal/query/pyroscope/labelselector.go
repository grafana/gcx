package pyroscope

import (
	"strings"

	promparser "github.com/prometheus/prometheus/promql/parser"
)

// LabelMatcher is a single label matcher from a Pyroscope label selector,
// e.g. the `service_name="frontend"` in `{service_name="frontend"}`.
type LabelMatcher struct {
	Key      string
	Operator string // one of =, !=, =~, !~
	Value    string
}

// ParseLabelSelector extracts the label matchers from a Pyroscope label
// selector (`{key="value", key2!="value2"}`), for building a Profiles
// Drilldown deep link. Returns ok=false if selector isn't a single,
// well-formed `{...}` block of matchers.
func ParseLabelSelector(selector string) ([]LabelMatcher, bool) {
	selector = strings.TrimSpace(selector)
	if !strings.HasPrefix(selector, "{") || !strings.HasSuffix(selector, "}") {
		return nil, false
	}
	if strings.TrimSpace(selector[1:len(selector)-1]) == "" {
		return nil, true
	}

	parsed, err := promparser.NewParser(promparser.Options{}).ParseMetricSelector(selector)
	if err != nil {
		return nil, false
	}

	matchers := make([]LabelMatcher, 0, len(parsed))
	for _, m := range parsed {
		matchers = append(matchers, LabelMatcher{Key: m.Name, Operator: m.Type.String(), Value: m.Value})
	}
	return matchers, true
}
