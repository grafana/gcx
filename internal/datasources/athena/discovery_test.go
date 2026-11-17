package athena_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/datasources/athena"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func discoveryCmds() map[string]func(*providers.ConfigLoader) *cobra.Command {
	return map[string]func(*providers.ConfigLoader) *cobra.Command{
		"list-catalogs":  athena.ListCatalogsCmd,
		"list-databases": athena.ListDatabasesCmd,
		"list-tables":    athena.ListTablesCmd,
		"describe-table": athena.DescribeTableCmd,
	}
}

func TestDiscoveryCmds_FilterAndLimitFlags(t *testing.T) {
	for name, build := range discoveryCmds() {
		t.Run(name, func(t *testing.T) {
			cmd := build(&providers.ConfigLoader{})

			filter := cmd.Flags().Lookup("filter")
			require.NotNil(t, filter)
			assert.Empty(t, filter.DefValue)

			limit := cmd.Flags().Lookup("limit")
			require.NotNil(t, limit)
			assert.Equal(t, "0", limit.DefValue, "default keeps the previous uncapped behaviour")

			assert.Contains(t, cmd.Annotations[agent.AnnotationLLMHint], "--filter", "agents discover --filter via the hint")
		})
	}
}

func TestDiscoveryCmds_NegativeLimitRejectedBeforeIO(t *testing.T) {
	for name, build := range discoveryCmds() {
		t.Run(name, func(t *testing.T) {
			cmd := build(&providers.ConfigLoader{})
			args := []string{"--limit", "-1"}
			if name == "describe-table" {
				args = append([]string{"my_table"}, args...)
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid --limit -1: must be >= 0")
		})
	}
}
