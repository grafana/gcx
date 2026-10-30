package root

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHelpTopicCompletionWithoutDefault(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	help := &cobra.Command{Use: "help", Run: func(*cobra.Command, []string) {}}
	cmd.AddCommand(help)
	cmd.SetHelpCommand(help)
	cmd.InitDefaultHelpCmd()
	require.Nil(t, help.ValidArgsFunction)
	require.Len(t, registerHelpTopics(cmd), 1)

	for _, tc := range []struct {
		name       string
		args       []string
		prefix     string
		wantTopics []cobra.Completion
	}{
		{name: "matching topic", prefix: "ex", wantTopics: []cobra.Completion{"exit-codes\tExplain exit codes with example scenarios."}},
		{name: "unmatched prefix", prefix: "other"},
		{name: "command arguments", args: []string{"config"}, prefix: "ex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completions, directive := help.ValidArgsFunction(help, tc.args, tc.prefix)
			assert.Equal(t, tc.wantTopics, completions)
			assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		})
	}
}
