//nolint:testpackage // Tests verify unexported command constructor wiring.
package metrics

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

func TestExprFlagSmoke_MetricsQuery(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantErr    string
		notSubstrs []string
	}{
		{
			name:       "--expr accepted instead of positional",
			args:       []string{"query", "--expr", "up"},
			notSubstrs: []string{"expression is required", "accepts"},
		},
		{
			name:    "both positional and --expr rejected",
			args:    []string{"query", "up", "--expr", "up"},
			wantErr: "not both",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := queryCmd(&providers.ConfigLoader{})
			err := execCmd(t, cmd, tt.args)
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
