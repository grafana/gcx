//nolint:testpackage // Tests verify unexported command constructor wiring.
package logs

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func execCmd(t *testing.T, cmd *cobra.Command, args []string) error {
	t.Helper()
	root := &cobra.Command{Use: "test"}
	root.AddCommand(cmd)
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(args)
	return root.Execute()
}

type exprSmokeCase struct {
	name       string
	args       []string
	wantErr    string
	notSubstrs []string
}

func runExprSmokeCases(t *testing.T, newCmd func() *cobra.Command, tests []exprSmokeCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := execCmd(t, newCmd(), tt.args)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			if err != nil {
				for _, s := range tt.notSubstrs {
					assert.NotContains(t, err.Error(), s)
				}
			}
		})
	}
}

func TestExprFlagSmoke_LogsQuery(t *testing.T) {
	newCmd := func() *cobra.Command { return queryCmd(&providers.ConfigLoader{}) }
	runExprSmokeCases(t, newCmd, []exprSmokeCase{
		{
			name:       "--expr accepted instead of positional",
			args:       []string{"query", "--expr", `{job="x"}`},
			notSubstrs: []string{"expression is required", "accepts"},
		},
		{
			name:    "both positional and --expr rejected",
			args:    []string{"query", `{job="x"}`, "--expr", `{job="x"}`},
			wantErr: "not both",
		},
	})
}

func TestExprFlagSmoke_LogsMetrics(t *testing.T) {
	newCmd := func() *cobra.Command { return metricsCmd(&providers.ConfigLoader{}) }
	runExprSmokeCases(t, newCmd, []exprSmokeCase{
		{
			name:       "--expr accepted instead of positional",
			args:       []string{"metrics", "--expr", `rate({job="x"}[5m])`},
			notSubstrs: []string{"expression is required", "accepts"},
		},
		{
			name:    "both positional and --expr rejected",
			args:    []string{"metrics", `rate({job="x"}[5m])`, "--expr", `rate({job="x"}[5m])`},
			wantErr: "not both",
		},
	})
}
