package kg_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/providers/kg"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: KGProvider implements framework.Setupable.
var _ framework.Setupable = (*kg.KGProvider)(nil)

func TestKGProvider_ProductName(t *testing.T) {
	p := &kg.KGProvider{}
	assert.Equal(t, "kg", p.ProductName())
}

func TestKGProvider_Status(t *testing.T) {
	p := &kg.KGProvider{}
	status, err := p.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "kg", status.Product)
	// No config keys → StateActive.
	assert.Equal(t, framework.StateActive, status.State)
}

func TestKGProvider_InfraCategories(t *testing.T) {
	p := &kg.KGProvider{}
	assert.Nil(t, p.InfraCategories())
}

func TestKGProvider_ResolveChoices(t *testing.T) {
	p := &kg.KGProvider{}
	choices, err := p.ResolveChoices(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, choices)
}

func TestKGProvider_ValidateSetup(t *testing.T) {
	p := &kg.KGProvider{}
	assert.NoError(t, p.ValidateSetup(context.Background(), nil))
}

func TestKGProvider_Setup(t *testing.T) {
	p := &kg.KGProvider{}
	err := p.Setup(context.Background(), nil)
	assert.True(t, errors.Is(err, framework.ErrSetupNotSupported))
}

func TestKGProvider_SetupCommand(t *testing.T) {
	p := &kg.KGProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	var setupCmd *cobra.Command
	for _, sub := range cmds[0].Commands() {
		if sub.Name() == "setup" {
			setupCmd = sub
			break
		}
	}
	require.NotNil(t, setupCmd, "expected 'setup' subcommand")

	stderr := &bytes.Buffer{}
	setupCmd.SetErr(stderr)
	err := setupCmd.RunE(setupCmd, nil)

	assert.True(t, errors.Is(err, framework.ErrSetupNotSupported))
	assert.NotEmpty(t, stderr.String())
}
