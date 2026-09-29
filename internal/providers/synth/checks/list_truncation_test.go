package checks_test

import (
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChecksListFiltersBeforeLimit checks that the --job and --label filters
// apply to the full set of checks before the limit. The fake API returns the
// checks in random order, so a filter after the limit loses matches.
func TestChecksListFiltersBeforeLimit(t *testing.T) {
	// 60 "web" checks and 55 "api" checks. Only the api checks have the
	// env=prod label. Ten of the api checks have team=core.
	st := &checkAPIState{checks: map[int64]checks.Check{}}
	for i := range 60 {
		id := int64(1000 + i)
		st.checks[id] = checks.Check{ID: id, Job: fmt.Sprintf("web-%d", i), Target: "https://example.com",
			Settings: checks.CheckSettings{"http": map[string]any{"method": "GET"}}}
	}
	for i := range 55 {
		id := int64(2000 + i)
		labels := []checks.Label{{Name: "env", Value: "prod"}}
		if i < 10 {
			labels = append(labels, checks.Label{Name: "team", Value: "core"})
		}
		st.checks[id] = checks.Check{ID: id, Job: fmt.Sprintf("api-%d", i), Target: "https://example.com",
			Labels: labels, Settings: checks.CheckSettings{"http": map[string]any{"method": "GET"}}}
	}
	srv := newCheckServer(t, st)

	tests := []struct {
		name     string
		args     []string
		wantLen  int
		wantHint string
	}{
		{
			name:    "job filter with fewer matches than the limit",
			args:    []string{"list", "-o", "json", "--job", "api-*", "--label", "team=core"},
			wantLen: 10,
		},
		{
			name:     "job filter with more matches than the limit",
			args:     []string{"list", "-o", "json", "--job", "api-*"},
			wantLen:  50,
			wantHint: "hint: showing first 50 of 55. See all results with: gcx synthetic-monitoring checks list -o json --job 'api-*' --limit 0",
		},
		{
			name:    "label filter with no limit",
			args:    []string{"list", "-o", "json", "--label", "env=prod", "--limit", "0"},
			wantLen: 55,
		},
		{
			name:     "no filter",
			args:     []string{"list", "-o", "json"},
			wantLen:  50,
			wantHint: "showing first 50 of 115",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.PinArgv(t, append([]string{"gcx", "synthetic-monitoring", "checks"}, tc.args...)...)
			stdout, stderr, err := runChecks(t, srv.URL, false, "", tc.args...)
			require.NoError(t, err)

			items, ok := decodeSingleJSONValue(t, stdout).([]any)
			require.True(t, ok, "stdout must stay a bare JSON array")
			assert.Len(t, items, tc.wantLen)
			if tc.wantHint == "" {
				assert.NotContains(t, stderr, "showing first")
			} else {
				assert.Contains(t, stderr, tc.wantHint)
			}
		})
	}
}
