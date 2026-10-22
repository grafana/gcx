package alert //nolint:testpackage // Tests inspect private native command factories.

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/grafana/gcx/internal/providers/native"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeAlertCommandAliases(t *testing.T) {
	intervalEnv(t)
	for _, cmd := range []*cobra.Command{
		newTimeIntervalsCommand(native.Fixed(native.Access{})),
		newTemplateGroupsCommand(native.Fixed(native.Access{})),
	} {
		t.Run(cmd.Name(), func(t *testing.T) {
			for _, pair := range [][2]string{{"list", "ls"}, {"delete", "rm"}} {
				canonical, _, err := cmd.Find([]string{pair[0]})
				require.NoError(t, err)
				alias, _, err := cmd.Find([]string{pair[1]})
				require.NoError(t, err)
				assert.Equal(t, pair[0], canonical.Name())
				assert.Same(t, canonical, alias)
			}
		})
	}
}

func TestTimeIntervalsExportUsesLegacyCommand(t *testing.T) {
	intervalEnv(t)
	loads := 0
	binding := native.Func(func(context.Context, native.LoadOptions) (native.Access, error) {
		loads++
		return native.Access{}, errors.New("unexpected load")
	})
	cmd := newTimeIntervalsCommand(binding)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"export"})
	require.ErrorContains(t, cmd.Execute(), `unknown command "export"`)
	assert.Zero(t, loads)
	assert.Contains(t, cmd.Long, "gcx alert mute-timings export")
	assert.Contains(t, cmd.Long, "--name <title>")
	assert.Contains(t, cmd.Long, "--format")
	assert.Contains(t, cmd.Long, "only Grafana-managed intervals")
	assert.Contains(t, cmd.Long, "not exported")

	alertCmd := (&AlertProvider{}).Commands()[0]
	legacy, remaining, err := alertCmd.Find([]string{"mute-timings", "export"})
	require.NoError(t, err)
	assert.Empty(t, remaining)
	assert.Equal(t, "export", legacy.Name())
	assert.Equal(t, "alert mute-timings export", legacy.CommandPath())
	assert.NotNil(t, legacy.Flags().Lookup("name"))
	formatFlag := legacy.Flags().Lookup("format")
	require.NotNil(t, formatFlag)
	assert.Equal(t, "yaml", formatFlag.DefValue)
}
