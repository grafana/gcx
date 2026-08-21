package elasticsearch_test

import (
	"testing"

	"github.com/grafana/gcx/internal/datasources/elasticsearch"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An explicit --index "" widens the request to every index, the opposite of
// what the flag is for, and does so silently. cmd.Flags().Changed is the only
// way to tell that apart from an omitted flag, which must keep meaning "all
// indices" as documented.
func TestMappingListCmds_RejectExplicitEmptyIndex(t *testing.T) {
	// A zero-value loader has no context/config wired up, so any code path
	// that reaches config loading or datasource resolution fails with an
	// unrelated error. Asserting on the specific validation message proves
	// this check runs first.
	loader := &providers.ConfigLoader{}

	cmds := map[string]func(*providers.ConfigLoader) *cobra.Command{
		"list-indices": elasticsearch.ListIndicesCmd,
		"list-fields":  elasticsearch.ListFieldsCmd,
	}

	for name, newCmd := range cmds {
		t.Run(name, func(t *testing.T) {
			cmd := newCmd(loader)
			cmd.SetArgs([]string{"--index="})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--index must not be empty")
		})

		t.Run(name+"/omitted index still means all", func(t *testing.T) {
			cmd := newCmd(loader)
			cmd.SetArgs([]string{})
			err := cmd.Execute()
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "--index must not be empty")
		})
	}
}
