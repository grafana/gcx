package sigil_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/grafana/gcx/internal/providers/sigil"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: SigilProvider implements framework.Setupable.
var _ framework.Setupable = (*sigil.SigilProvider)(nil)

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

func TestSigilProvider_ProductName(t *testing.T) {
	p := &sigil.SigilProvider{}
	assert.Equal(t, "sigil", p.ProductName())
}

func TestSigilProvider_Status(t *testing.T) {
	p := &sigil.SigilProvider{}
	status, err := p.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "sigil", status.Product)
	// No config keys → StateActive.
	assert.Equal(t, framework.StateActive, status.State)
}

func TestSigilProvider_InfraCategories(t *testing.T) {
	p := &sigil.SigilProvider{}
	assert.Nil(t, p.InfraCategories())
}

func TestSigilProvider_ResolveChoices(t *testing.T) {
	p := &sigil.SigilProvider{}
	choices, err := p.ResolveChoices(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, choices)
}

func TestSigilProvider_ValidateSetup(t *testing.T) {
	p := &sigil.SigilProvider{}
	assert.NoError(t, p.ValidateSetup(context.Background(), nil))
}

func TestSigilProvider_Setup(t *testing.T) {
	p := &sigil.SigilProvider{}
	err := p.Setup(context.Background(), nil)
	require.ErrorIs(t, err, framework.ErrSetupNotSupported)
}

func TestSigilProvider_SetupCommand(t *testing.T) {
	p := &sigil.SigilProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	setupCmd := findSubcommand(cmds[0], "setup")
	require.NotNil(t, setupCmd, "expected 'setup' subcommand")

	stderr := &bytes.Buffer{}
	setupCmd.SetErr(stderr)
	err := setupCmd.RunE(setupCmd, nil)

	require.ErrorIs(t, err, framework.ErrSetupNotSupported)
	assert.NotEmpty(t, stderr.String())
}
