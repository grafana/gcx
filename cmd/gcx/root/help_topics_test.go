package root_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExitCodesHelpTopic(t *testing.T) {
	cmd := root.NewCommandForTest("test", nil)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--no-color", "help", "exit-codes"})
	require.NoError(t, cmd.Execute())
	assert.Empty(t, stderr.String())
	assert.NotContains(t, stdout.String(), "Unknown help topic")
	for _, info := range gcxerrors.ExitCodes() {
		assert.Contains(t, stdout.String(), fmt.Sprintf("  %d  %s\n", info.Code, info.Name))
		assert.Contains(t, stdout.String(), info.Description)
		assert.Contains(t, stdout.String(), "Example: "+info.Example)
	}
}

func TestExitCodesAdditionalHelpTopic(t *testing.T) {
	for _, styled := range []bool{false, true} {
		t.Run(fmt.Sprintf("styled=%t", styled), func(t *testing.T) {
			style.SetEnabled(styled)
			t.Cleanup(func() { style.SetEnabled(true) })
			for _, args := range [][]string{{"--help"}, {"help"}} {
				cmd := root.NewCommandForTest("test", nil)
				var stdout bytes.Buffer
				cmd.SetOut(&stdout)
				cmd.SetArgs(args)
				require.NoError(t, cmd.Execute())
				assert.Contains(t, stdout.String(), "Additional help topics:\n  gcx help exit-codes  Explain exit codes with example scenarios.")
			}
		})
	}
}

func TestExitCodesIsNotCommand(t *testing.T) {
	cmd := root.NewCommandForTest("test", nil)
	_, _, err := cmd.Find([]string{"exit-codes"})
	require.ErrorContains(t, err, `unknown command "exit-codes"`)
	cmd.SetArgs([]string{"exit-codes"})
	err = cmd.Execute()
	require.ErrorContains(t, err, `unknown command "exit-codes"`)
	detail := fail.ErrorToDetailedError(err)
	require.NotNil(t, detail)
	require.NotNil(t, detail.ExitCode)
	assert.Equal(t, gcxerrors.ExitUsageError, *detail.ExitCode)
}

func TestExitCodesNotInCommands(t *testing.T) {
	for _, includeHidden := range []bool{false, true} {
		t.Run(fmt.Sprintf("include-hidden=%t", includeHidden), func(t *testing.T) {
			cmd := root.NewCommandForTest("test", nil)
			var stdout bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetArgs([]string{"commands", "--flat", "-o", "json", fmt.Sprintf("--include-hidden=%t", includeHidden)})
			require.NoError(t, cmd.Execute())
			assert.NotContains(t, stdout.String(), "exit-codes")
		})
	}
}

func TestHelpCommandDelegates(t *testing.T) {
	for _, path := range [][]string{{"version"}, {"config", "unset"}} {
		t.Run(fmt.Sprint(path), func(t *testing.T) {
			outputs := make([]string, 0, 2)
			for _, args := range [][]string{append([]string{"help"}, path...), append(append([]string{}, path...), "--help")} {
				cmd := root.NewCommandForTest("test", nil)
				var stdout bytes.Buffer
				cmd.SetOut(&stdout)
				cmd.SetArgs(append([]string{"--no-color"}, args...))
				require.NoError(t, cmd.Execute())
				outputs = append(outputs, stdout.String())
			}
			assert.Equal(t, outputs[1], outputs[0])
		})
	}

	t.Run("unknown topic", func(t *testing.T) {
		cmd := root.NewCommandForTest("test", nil)
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetArgs([]string{"help", "no-such-topic"})
		require.NoError(t, cmd.Execute()) // Cobra reports unknown topics without returning an error.
		assert.Contains(t, stdout.String(), fmt.Sprintf("Unknown help topic %#q", []string{"no-such-topic"}))
	})
}

func TestHelpTopicCompletion(t *testing.T) {
	cmd := root.NewCommandForTest("test", nil)
	// Reinitialization must preserve the topic handler and completion.
	cmd.InitDefaultHelpCmd()
	help, _, err := cmd.Find([]string{"help"})
	require.NoError(t, err)
	completions, directive := help.ValidArgsFunction(help, nil, "ex")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
	assert.Contains(t, completions, "exit-codes\tExplain exit codes with example scenarios.")
	completions, _ = help.ValidArgsFunction(help, []string{"config"}, "un")
	assert.Contains(t, completions, "unset\tUnset a single value in a configuration file")
	completions, _ = help.ValidArgsFunction(help, nil, "ver")
	assert.NotContains(t, completions, "exit-codes\tExplain exit codes with example scenarios.")
}
