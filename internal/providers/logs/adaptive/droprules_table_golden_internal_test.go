package logs

import (
	"bytes"
	"testing"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestDropRulesTableGolden(t *testing.T) {
	testutils.SetAgentMode(t, false)
	rows := []DropRule{
		{ID: "rule-1", Name: "Checkout", SegmentID: "segment-1", Version: 2, ExpiresAt: "2027-01-01T00:00:00Z", CreatedAt: "2026-09-01T12:00:00Z", UpdatedAt: "2026-09-02T12:00:00Z"},
		{ID: "rule-2", Name: "Disabled", SegmentID: "segment-2", Version: 1, Disabled: true},
	}
	for _, name := range []string{"table", "wide"} {
		for _, tc := range []struct {
			name    string
			payload any
			setup   func(*cobra.Command) *cmdio.Options
		}{
			{"list_rows", rows, func(cmd *cobra.Command) *cmdio.Options { o := &dropRulesListOpts{}; o.setup(cmd); return &o.IO }},
			{"list_empty", []DropRule{}, func(cmd *cobra.Command) *cmdio.Options { o := &dropRulesListOpts{}; o.setup(cmd); return &o.IO }},
			{"get", rows[0], func(cmd *cobra.Command) *cmdio.Options { o := &dropRulesGetOpts{}; o.setup(cmd); return &o.IO }},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				cmd := &cobra.Command{}
				opts := tc.setup(cmd)
				require.NoError(t, cmd.Flags().Set("output", name))
				require.NoError(t, opts.Validate())
				var buf bytes.Buffer
				require.NoError(t, opts.Encode(&buf, tc.payload))
				testutils.Golden(t, "drop_rules_"+name+"_"+tc.name, buf.String())
			})
		}
	}
}
