package faro_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/grafana/gcx/internal/providers/faro"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: FaroProvider implements framework.Setupable.
var _ framework.Setupable = (*faro.FaroProvider)(nil)

func TestFaroProvider_ProductName(t *testing.T) {
	p := &faro.FaroProvider{}
	assert.Equal(t, "faro", p.ProductName())
}

func TestFaroProvider_Status(t *testing.T) {
	p := &faro.FaroProvider{}
	status, err := p.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "faro", status.Product)
	assert.NotEmpty(t, string(status.State))
}

func TestFaroProvider_InfraCategories(t *testing.T) {
	p := &faro.FaroProvider{}
	assert.Nil(t, p.InfraCategories())
}

func TestFaroProvider_ResolveChoices(t *testing.T) {
	p := &faro.FaroProvider{}
	choices, err := p.ResolveChoices(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, choices)
}

func TestFaroProvider_ValidateSetup(t *testing.T) {
	p := &faro.FaroProvider{}
	assert.NoError(t, p.ValidateSetup(context.Background(), nil))
}

func TestFaroProvider_Setup(t *testing.T) {
	p := &faro.FaroProvider{}
	err := p.Setup(context.Background(), nil)
	require.ErrorIs(t, err, framework.ErrSetupNotSupported)
}

func TestFaroProvider_SetupCommand(t *testing.T) {
	p := &faro.FaroProvider{}
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

	require.ErrorIs(t, err, framework.ErrSetupNotSupported)
	assert.NotEmpty(t, stderr.String())
}
