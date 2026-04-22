package aio11y_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/aio11y"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAIO11yProvider_Interface(t *testing.T) {
	p := &aio11y.AIO11yProvider{}

	assert.Equal(t, "aio11y", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.NoError(t, p.Validate(nil))
	assert.NoError(t, p.Validate(map[string]string{}))
	assert.Nil(t, p.ConfigKeys())
}

func TestAIO11yProvider_Commands(t *testing.T) {
	p := &aio11y.AIO11yProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	aio11yCmd := cmds[0]
	assert.Equal(t, "aio11y", aio11yCmd.Use)

	subNames := commandNames(aio11yCmd)
	for _, exp := range []string{"conversations", "agents", "evaluators", "rules"} {
		assert.Contains(t, subNames, exp)
	}

	convsCmd := findSubcommand(aio11yCmd, "conversations")
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
