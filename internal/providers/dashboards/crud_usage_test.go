package dashboards_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/dashboards"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardInvalidOptionsAreUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		detail string
	}{
		{"negative limit", []string{"list", "--limit", "-1"}, "--limit must be >= 0"},
		{"continue without limit", []string{"list", "--continue", "next", "--limit", "0"}, "--continue requires --limit > 0"},
		{"unknown output", []string{"get", "some-uid", "-o", "potato"}, "unknown output format 'potato'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "gcx", SilenceErrors: true, SilenceUsage: true}
			provider := &dashboards.DashboardsProvider{}
			cmd.AddCommand(provider.Commands()...)
			cmd.SetArgs(append([]string{"dashboards"}, tc.args...))
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			err := cmd.Execute()
			var detailed *gcxerrors.DetailedError
			require.ErrorAs(t, err, &detailed)
			assert.Equal(t, "Invalid command usage", detailed.Summary)
			require.NotNil(t, detailed.ExitCode)
			assert.Equal(t, gcxerrors.ExitUsageError, *detailed.ExitCode)
			require.ErrorContains(t, detailed.Parent, tc.detail)
			assert.Contains(t, detailed.Suggestions[0], "gcx dashboards "+tc.args[0]+" --help")
			assert.Empty(t, output.String())
		})
	}
}
