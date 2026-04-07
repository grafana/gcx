package sigil_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/sigil"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSigilProvider_Interface(t *testing.T) {
	p := &sigil.SigilProvider{}

	assert.Equal(t, "sigil", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.NoError(t, p.Validate(nil))
	assert.NoError(t, p.Validate(map[string]string{}))
	assert.Nil(t, p.ConfigKeys())
}

func TestSigilProvider_Commands(t *testing.T) {
	p := &sigil.SigilProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	sigilCmd := cmds[0]
	assert.Equal(t, "sigil", sigilCmd.Use)

	subNames := commandNames(sigilCmd)
	for _, exp := range []string{"conversations", "agents", "evaluators", "rules"} {
		assert.Contains(t, subNames, exp)
	}

	convsCmd := findSubcommand(sigilCmd, "conversations")
	require.NotNil(t, convsCmd)

	convSubNames := commandNames(convsCmd)
	for _, exp := range []string{"show", "search"} {
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
