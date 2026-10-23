package alert

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestGroupRulesTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	for _, tc := range []struct {
		name  string
		rules []RuleStatus
	}{
		{"rows", []RuleStatus{{UID: "rule-1", Name: "High latency", State: StateFiring, Health: "ok"}, {UID: "rule-2", Name: "Low traffic", State: StateInactive, Health: "error", IsPaused: true}}},
		{"empty", []RuleStatus{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := &groupsGetOpts{}
			flags := pflag.NewFlagSet("get", pflag.ContinueOnError)
			opts.setup(flags)
			require.NoError(t, opts.IO.Validate())
			var buf bytes.Buffer
			require.NoError(t, opts.IO.Encode(&buf, &RuleGroup{Name: "production", Rules: tc.rules}))
			testutils.Golden(t, "group_rules_"+tc.name, buf.String())
		})
	}
}
