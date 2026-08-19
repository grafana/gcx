package agento11y_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/agento11y"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgento11yProvider_Interface(t *testing.T) {
	p := &agento11y.Agento11yProvider{}

	assert.Equal(t, "agento11y", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.NoError(t, p.Validate(nil))
	assert.NoError(t, p.Validate(map[string]string{}))
	assert.Nil(t, p.ConfigKeys())
}

func TestAgento11yProvider_Commands(t *testing.T) {
	p := &agento11y.Agento11yProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	agento11yCmd := cmds[0]
	assert.Equal(t, "agento11y", agento11yCmd.Use)
	assert.Empty(t, agento11yCmd.Aliases)

	subNames := commandNames(agento11yCmd)
	for _, exp := range []string{"conversations", "agents", "evaluators", "rules", "guards"} {
		assert.Contains(t, subNames, exp)
	}

	convsCmd := findSubcommand(agento11yCmd, "conversations")
	require.NotNil(t, convsCmd)

	convSubNames := commandNames(convsCmd)
	for _, exp := range []string{"list", "get", "search"} {
		assert.Contains(t, convSubNames, exp)
	}
}

func commandNames(cmd *cobra.Command) []string {
	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	return names
}

func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, sub := range parent.Commands() {
		if sub.Name() == name {
			return sub
		}
	}
	return nil
}
