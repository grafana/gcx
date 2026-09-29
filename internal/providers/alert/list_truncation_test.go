package alert_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// namedItems returns a JSON array of n objects with uid and name fields.
func namedItems(prefix string, n int) []map[string]string {
	items := make([]map[string]string, n)
	for i := range items {
		id := fmt.Sprintf("%s-%d", prefix, i)
		items[i] = map[string]string{"uid": id, "name": id}
	}
	return items
}

// ruleGroups returns the given number of groups. Each group holds perGroup rules.
func ruleGroups(groups, perGroup int) []alert.RuleGroup {
	out := make([]alert.RuleGroup, groups)
	for g := range out {
		out[g].Name = fmt.Sprintf("group-%d", g)
		for r := range perGroup {
			out[g].Rules = append(out[g].Rules, alert.RuleStatus{
				UID:   fmt.Sprintf("uid-%d-%d", g, r),
				Name:  fmt.Sprintf("rule-%d-%d", g, r),
				State: alert.StateInactive,
			})
		}
	}
	return out
}

// TestAlertListTruncation checks that each alert list command reports a
// truncated page on stderr and stays silent for a complete set.
func TestAlertListTruncation(t *testing.T) {
	// Five items (or five groups) for each command.
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/contact-points"):
			writeJSON(w, namedItems("cp", 5))
		case strings.HasSuffix(r.URL.Path, "/templates"):
			writeJSON(w, namedItems("tpl", 5))
		case strings.HasSuffix(r.URL.Path, "/mute-timings"):
			writeJSON(w, namedItems("mt", 5))
		default:
			serveRules(ruleGroups(5, 1))(w, r)
		}
	}

	commands := []struct {
		name   string
		newCmd func(alert.GrafanaConfigLoader) *cobra.Command
	}{
		{"groups", alert.NewGroupsListCommandForTest},
		{"contact-points", alert.NewContactPointsListCommandForTest},
		{"templates", alert.NewTemplatesListCommandForTest},
		{"mute-timings", alert.NewMuteTimingsListCommandForTest},
		{"rules", alert.NewRulesListCommandForTest},
	}
	limits := []struct {
		name     string
		limit    string
		wantLen  int
		wantHint string
	}{
		{"truncated", "2", 2, "hint: showing first 2 of 5. See all results with: gcx alert list -o json --limit 0"},
		{"limit equals total", "5", 5, ""},
		{"no limit", "0", 5, ""},
	}

	for _, c := range commands {
		for _, l := range limits {
			t.Run(c.name+"/"+l.name, func(t *testing.T) {
				setAgentMode(t, false)
				testutils.PinArgv(t, "gcx", "alert", "list", "-o", "json", "--limit", l.limit)
				loader := newAlertFixture(t, handler)

				stdout, stderr, err := runCmdSplit(t, c.newCmd(loader), []string{"list", "-o", "json", "--limit", l.limit}, "")
				require.NoError(t, err)

				doc, ok := decodeSingleJSONDocument(t, stdout).([]any)
				require.True(t, ok, "stdout must stay a bare JSON array")
				assert.Len(t, doc, l.wantLen)
				if l.wantHint == "" {
					assert.NotContains(t, stderr, "showing first")
				} else {
					assert.Contains(t, stderr, l.wantHint)
				}
			})
		}
	}
}

// TestRulesListLimitCountsRules checks that --limit counts rules, not groups,
// in the table and in JSON output.
func TestRulesListLimitCountsRules(t *testing.T) {
	tests := []struct {
		name       string
		format     string
		limit      string
		wantRules  int
		wantGroups int
		wantHint   bool
	}{
		{"json truncated inside a group", "json", "4", 4, 2, true},
		{"json all rules", "json", "6", 6, 2, false},
		{"table truncated", "table", "4", 4, 0, true},
		{"table all rules", "table", "0", 6, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, false)
			testutils.PinArgv(t, "gcx", "alert", "rules", "list", "--limit", tc.limit)
			// Two groups of three rules, plus one empty group.
			groups := append(ruleGroups(2, 3), alert.RuleGroup{Name: "empty"})
			loader := newAlertFixture(t, serveRules(groups))

			stdout, stderr, err := runCmdSplit(t, alert.NewRulesListCommandForTest(loader),
				[]string{"list", "-o", tc.format, "--limit", tc.limit}, "")
			require.NoError(t, err)

			if tc.format == "json" {
				var got []alert.RuleGroup
				require.NoError(t, json.Unmarshal([]byte(stdout), &got))
				assert.Len(t, got, tc.wantGroups)
				n := 0
				for _, g := range got {
					n += len(g.Rules)
				}
				assert.Equal(t, tc.wantRules, n)
			} else {
				assert.Equal(t, tc.wantRules, strings.Count(stdout, "uid-"))
			}

			if tc.wantHint {
				assert.Contains(t, stderr, fmt.Sprintf("showing first %s of 6", tc.limit))
				assert.Contains(t, stderr, "gcx alert rules list --limit 0")
			} else {
				assert.NotContains(t, stderr, "showing first")
			}
		})
	}
}
