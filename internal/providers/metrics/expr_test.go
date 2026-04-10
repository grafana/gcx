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
	t.Run("--expr accepted instead of positional", func(t *testing.T) {
		cmd := queryCmd(&providers.ConfigLoader{})
		err := execCmd(t, cmd, []string{"query", "--expr", "up"})
		if err != nil {
			assert.NotContains(t, err.Error(), "expression is required")
			assert.NotContains(t, err.Error(), "accepts")
		}
	})

	t.Run("both positional and --expr rejected", func(t *testing.T) {
		cmd := queryCmd(&providers.ConfigLoader{})
		err := execCmd(t, cmd, []string{"query", "up", "--expr", "up"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not both")
	})
}
