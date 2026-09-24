package prometheus

import (
	"errors"
	"fmt"
	"strings"

	"github.com/prometheus/common/model"
	promlabels "github.com/prometheus/prometheus/model/labels"
	promparser "github.com/prometheus/prometheus/promql/parser"
	"github.com/spf13/cobra"
)

// foldMetricNameSelector folds metric into every match selector as an equal
// __name__ matcher, so metric always narrows. It is a thin wrapper over
// foldNameMatcherSelector for the labels command's --metric flag, which has
// no regex counterpart.
func foldMetricNameSelector(flagName, metric string, match []string) ([]string, error) {
	var nameMatcher *promlabels.Matcher
	if metric != "" {
		nameMatcher = newEqualNameMatcher(metric)
	}
	return foldNameMatcherSelector(flagName, nameMatcher, match)
}

// newEqualNameMatcher builds a __name__="metric" matcher for the labels
// command's --metric flag and the search commands' --metric flag.
func newEqualNameMatcher(metric string) *promlabels.Matcher {
	return promlabels.MustNewMatcher(promlabels.MatchEqual, model.MetricNameLabel, metric)
}

// newRegexNameMatcher builds a __name__=~"pattern" matcher for
// --metric-regex. pattern is used exactly as given — not escaped, not wrapped.
func newRegexNameMatcher(pattern string) (*promlabels.Matcher, error) {
	m, err := promlabels.NewMatcher(promlabels.MatchRegexp, model.MetricNameLabel, pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid --metric-regex pattern %q: %w", pattern, err)
	}
	return m, nil
}

// resolveNameMatcher builds the __name__ matcher for the search commands'
// --metric / --metric-regex flags, which are mutually exclusive. It returns
// (nil, "", nil) when neither is set.
func resolveNameMatcher(metric, metricRegex string) (*promlabels.Matcher, string, error) {
	switch {
	case metric != "" && metricRegex != "":
		return nil, "", errors.New("--metric and --metric-regex are mutually exclusive")
	case metric != "":
		return newEqualNameMatcher(metric), "metric", nil
	case metricRegex != "":
		m, err := newRegexNameMatcher(metricRegex)
		if err != nil {
			return nil, "", err
		}
		return m, "metric-regex", nil
	default:
		return nil, "", nil
	}
}

// foldNameMatcherSelector folds nameMatcher into every match selector, so it
// always narrows. A nil nameMatcher validates match's syntax and returns it
// unchanged. Repeated match selectors remain a union: the Prometheus/Mimir
// API returns results from series matching any match[] parameter. flagName
// names the offending flag in error messages (e.g. "metric" for the labels
// command's or search commands' --metric, "metric-regex" for the search
// commands' --metric-regex).
func foldNameMatcherSelector(flagName string, nameMatcher *promlabels.Matcher, match []string) ([]string, error) {
	parser := promparser.NewParser(promparser.Options{})

	if nameMatcher == nil {
		// Validate client-side so a selector typo fails here with a clear
		// error instead of an opaque proxied 400; valid selectors are sent
		// exactly as written.
		for _, sel := range match {
			if _, err := parser.ParseMetricSelector(sel); err != nil {
				return nil, fmt.Errorf("invalid --match selector %q: %w", sel, err)
			}
		}
		return match, nil
	}

	if len(match) == 0 {
		return []string{"{" + nameMatcher.String() + "}"}, nil
	}

	folded := make([]string, 0, len(match))
	for _, sel := range match {
		matchers, err := parser.ParseMetricSelector(sel)
		if err != nil {
			return nil, fmt.Errorf("invalid --match selector %q: %w", sel, err)
		}

		// A selector may already constrain __name__ (a bare metric name or an
		// explicit matcher).
		redundant := false
		for _, m := range matchers {
			if m.Name != model.MetricNameLabel {
				continue
			}
			if nameMatcher.Type == promlabels.MatchEqual {
				// A concrete metric name: consistent constraints (same
				// metric, regex superset) fold fine; a constraint the
				// metric cannot satisfy would silently match nothing, so
				// reject it instead.
				if !m.Matches(nameMatcher.Value) {
					return nil, fmt.Errorf("--%s %q contradicts the __name__ matcher in --match selector %q: the intersection matches nothing", flagName, nameMatcher.Value, sel)
				}
				if m.Type == promlabels.MatchEqual {
					redundant = true
				}
				continue
			}
			// nameMatcher is itself a regex: whether two regexes'
			// languages intersect is undecidable in general, so only an
			// exact duplicate is treated as redundant. Anything else is
			// folded in as an additional __name__ matcher — valid PromQL,
			// ANDed with the existing one.
			if m.Type == nameMatcher.Type && m.Value == nameMatcher.Value {
				redundant = true
			}
		}

		// Brace-joining is safe despite Pattern 14's no-string-formatting rule:
		// every part is a canonical Matcher.String() rendering, and
		// promql-builder cannot express a matcher-only selector.
		parts := make([]string, 0, len(matchers)+1)
		for _, m := range matchers {
			parts = append(parts, m.String())
		}
		if !redundant {
			parts = append(parts, nameMatcher.String())
		}

		folded = append(folded, "{"+strings.Join(parts, ",")+"}")
	}
	return folded, nil
}

// rejectExplicitlyEmptyFlags returns an error if any named flag was passed
// an explicitly empty value (typically an unset shell variable, as in
// --metric "$METRIC") — a case that would otherwise silently answer a
// different question than the one asked, rather than failing loudly.
func rejectExplicitlyEmptyFlags(cmd *cobra.Command, names ...string) error {
	for _, name := range names {
		if cmd.Flags().Changed(name) && cmd.Flags().Lookup(name).Value.String() == "" {
			return fmt.Errorf("invalid --%s: value is empty (unset shell variable?)", name)
		}
	}
	return nil
}
