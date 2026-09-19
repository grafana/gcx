package root_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProfilesExploreFlags(t *testing.T) {
	for _, path := range [][]string{
		{"profiles", "query"}, {"profiles", "metrics"},
		{"datasources", "pyroscope", "query"}, {"datasources", "pyroscope", "metrics"},
	} {
		t.Run(path[0]+"/"+path[len(path)-1], func(t *testing.T) {
			cmd, remaining, err := root.Command("test").Find(path)
			require.NoError(t, err)
			require.Empty(t, remaining)
			for _, flag := range []string{"share-link", "open"} {
				f := cmd.Flags().Lookup(flag)
				require.NotNil(t, f)
				assert.Equal(t, "bool", f.Value.Type())
				assert.Equal(t, "false", f.DefValue)
			}
		})
	}
}
