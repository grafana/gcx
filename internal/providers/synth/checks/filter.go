package checks

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CheckFilter holds criteria for filtering checks and status results.
// A nil filter matches everything.
type CheckFilter struct {
	Labels     map[string]string
	JobPattern string
	StatusStr  string
}

// Validate checks that the filter's patterns are syntactically valid.
// Call this before using MatchCheck to fail fast on bad patterns.
func (f *CheckFilter) Validate() error {
	if f == nil {
		return nil
	}
	if f.JobPattern != "" {
		if _, err := filepath.Match(f.JobPattern, ""); err != nil {
			return fmt.Errorf("invalid --job pattern %q: %w", f.JobPattern, err)
		}
	}
	return nil
}

// MatchCheck returns true if the check matches the filter's label and job criteria.
// Status filtering is not available here — use MatchResult after Prometheus data is available.
func (f *CheckFilter) MatchCheck(c Check) bool {
	if f == nil {
		return true
	}

	if f.JobPattern != "" {
		matched, err := filepath.Match(f.JobPattern, c.Job)
		if err != nil || !matched {
			return false
		}
	}

	for k, v := range f.Labels {
		if !hasLabel(c.Labels, k, v) {
			return false
		}
	}

	return true
}

// MatchResult returns true if the status result matches the filter's status criteria.
func (f *CheckFilter) MatchResult(r CheckStatusResult) bool {
	if f == nil || f.StatusStr == "" {
		return true
	}
	return strings.EqualFold(r.Status, f.StatusStr)
}

// hasLabel returns true if the label set contains the key/value pair.
func hasLabel(labels []Label, key, value string) bool {
	for _, l := range labels {
		if l.Name == key && l.Value == value {
			return true
		}
	}
	return false
}

// ParseLabelFlags parses a slice of "key=value" strings into a map.
// Returns an error if any entry is not in key=value format.
func ParseLabelFlags(vals []string) (map[string]string, error) {
	m := make(map[string]string, len(vals))
	for _, v := range vals {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			return nil, fmt.Errorf("invalid label %q: expected key=value format", v)
		}
		m[parts[0]] = parts[1]
	}
	return m, nil
}
