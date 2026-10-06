package profiles_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/commands"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/profiles"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdaptiveUnsupportedCommand(t *testing.T) {
	const docs = "https://grafana.com/docs/grafana-cloud/observe-and-act/adaptive-telemetry/adaptive-profiles/"
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "unsupported", args: []string{"adaptive"}},
		{name: "reject positional", args: []string{"adaptive", "enable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "gcx"}
			cmd.AddCommand((&profiles.Provider{}).Commands()[0])
			var out, errOut bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs(append([]string{"profiles"}, tc.args...))
			err := cmd.Execute()
			require.Error(t, err)
			assert.Empty(t, out.String())
			assert.Empty(t, errOut.String())
			if len(tc.args) == 1 {
				var detailed *gcxerrors.DetailedError
				require.ErrorAs(t, err, &detailed)
				assert.Equal(t, "Adaptive Profiles management is not supported by gcx", detailed.Summary)
				assert.Equal(t, docs, detailed.DocsLink)
				assert.Contains(t, detailed.Suggestions, "Use Adaptive Profiles in the Grafana Cloud UI.")
				assert.NotContains(t, err.Error(), "not yet available")
			} else {
				assert.Contains(t, err.Error(), "unknown command")
			}
		})
	}
}

func TestAdaptiveHelpMetadata(t *testing.T) {
	cmd := &cobra.Command{Use: "gcx"}
	cmd.AddCommand((&profiles.Provider{}).Commands()[0])
	adaptive, _, err := cmd.Find([]string{"profiles", "adaptive"})
	require.NoError(t, err)
	assert.Equal(t, "Adaptive Profiles management is not supported by gcx", adaptive.Short)
	assert.Contains(t, adaptive.Long, "Adaptive Profiles is available in Grafana Cloud")
	assert.NotContains(t, adaptive.Short, "not yet available")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"profiles", "adaptive", "--help"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "gcx does not support its management commands")
	assert.Contains(t, out.String(), "https://grafana.com/docs/grafana-cloud/observe-and-act/adaptive-telemetry/adaptive-profiles/")
}

func TestAdaptiveCatalogMetadata(t *testing.T) {
	root := &cobra.Command{Use: "gcx"}
	root.AddCommand((&profiles.Provider{}).Commands()[0])
	root.AddCommand(commands.Command(root))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"commands", "--flat", "-o", "json"})
	require.NoError(t, root.Execute())
	var catalog commands.FlatCatalogOutput
	require.NoError(t, json.Unmarshal(out.Bytes(), &catalog))
	var found bool
	for _, item := range catalog.Commands {
		if item.FullPath == "gcx profiles adaptive" {
			found = true
			assert.Equal(t, "Adaptive Profiles management is not supported by gcx", item.Description)
			assert.Contains(t, item.Long, "Use the Grafana Cloud UI")
			assert.NotContains(t, item.Description, "not yet available")
		}
	}
	require.True(t, found, "adaptive command must remain discoverable")
}
