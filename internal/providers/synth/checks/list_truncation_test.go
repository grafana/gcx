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
		name      string
		args      []string
		wantLen   int
		wantTotal int // 0 means that list_meta must be absent
		wantHint  string
	}{
		{
			name:    "job filter with fewer matches than the limit",
			args:    []string{"list", "-o", "json", "--job", "api-*", "--label", "team=core"},
			wantLen: 10,
		},
		{
			name:      "job filter with more matches than the limit",
			args:      []string{"list", "-o", "json", "--job", "api-*"},
			wantLen:   50,
			wantTotal: 55,
			wantHint:  "hint: showing first 50 of 55. See all results with: gcx synthetic-monitoring checks list -o json --job 'api-*' --limit 0",
		},
		{
			name:    "label filter with no limit",
			args:    []string{"list", "-o", "json", "--label", "env=prod", "--limit", "0"},
			wantLen: 55,
		},
		{
			name:      "no filter",
			args:      []string{"list", "-o", "json"},
			wantLen:   50,
			wantTotal: 115,
			wantHint:  "showing first 50 of 115",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.PinArgv(t, append([]string{"gcx", "synthetic-monitoring", "checks"}, tc.args...)...)
			stdout, stderr, err := runChecks(t, srv.URL, false, "", tc.args...)
			require.NoError(t, err)

			page := testutils.DecodeListPage(t, stdout)
			assert.Len(t, page.Items, tc.wantLen)
			if tc.wantTotal == 0 {
				assert.Nil(t, page.ListMeta, "a complete set must not carry list_meta")
			} else {
				require.NotNil(t, page.ListMeta, "a truncated page must carry list_meta")
				assert.Equal(t, tc.wantLen, page.ListMeta.Returned)
				require.NotNil(t, page.ListMeta.Total)
				assert.Equal(t, tc.wantTotal, *page.ListMeta.Total)
				assert.Contains(t, page.ListMeta.Continue, "--limit 0")
			}
			if tc.wantHint == "" {
				assert.NotContains(t, stderr, "showing first")
			} else {
				assert.Contains(t, stderr, tc.wantHint)
			}
		})
	}
}

// TestChecksListEnvelopeFieldSelection checks that --json field selection
// applies to the items of the envelope and keeps list_meta.
func TestChecksListEnvelopeFieldSelection(t *testing.T) {
	st := &checkAPIState{checks: map[int64]checks.Check{}}
	for i := range 3 {
		id := int64(1000 + i)
		st.checks[id] = checks.Check{ID: id, Job: fmt.Sprintf("web-%d", i), Target: "https://example.com",
			Settings: checks.CheckSettings{"http": map[string]any{"method": "GET"}}}
	}
	srv := newCheckServer(t, st)

	tests := []struct {
		name     string
		args     []string
		wantLen  int
		wantMeta bool
	}{
		{"truncated", []string{"list", "--json", "spec.job", "--limit", "2"}, 2, true},
		{"complete", []string{"list", "--json", "spec.job", "--limit", "0"}, 3, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutils.PinArgv(t, append([]string{"gcx", "synthetic-monitoring", "checks"}, tc.args...)...)
			stdout, _, err := runChecks(t, srv.URL, false, "", tc.args...)
			require.NoError(t, err)

			page := testutils.DecodeListPage(t, stdout)
			require.Len(t, page.Items, tc.wantLen)
			for _, item := range page.Items {
				assert.Equal(t, []string{"spec.job"}, keysOf(item), "selection must apply to each item")
			}
			assert.Equal(t, tc.wantMeta, page.ListMeta != nil)
		})
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
