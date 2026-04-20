package irm_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: IRMProvider implements framework.Setupable.
var _ framework.Setupable = (*irm.IRMProvider)(nil)

func TestIRMProvider_ProductName(t *testing.T) {
	p := &irm.IRMProvider{}
	assert.Equal(t, "irm", p.ProductName())
}

func TestIRMProvider_Status(t *testing.T) {
	p := &irm.IRMProvider{}
	status, err := p.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "irm", status.Product)
	assert.NotEmpty(t, string(status.State))
}

func TestIRMProvider_InfraCategories(t *testing.T) {
	p := &irm.IRMProvider{}
	assert.Nil(t, p.InfraCategories())
}

func TestIRMProvider_ResolveChoices(t *testing.T) {
	p := &irm.IRMProvider{}
	choices, err := p.ResolveChoices(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, choices)
}

func TestIRMProvider_ValidateSetup(t *testing.T) {
	p := &irm.IRMProvider{}
	assert.NoError(t, p.ValidateSetup(context.Background(), nil))
}

func TestIRMProvider_Setup(t *testing.T) {
	p := &irm.IRMProvider{}
	err := p.Setup(context.Background(), nil)
	assert.True(t, errors.Is(err, framework.ErrSetupNotSupported))
}

func TestIRMProvider_SetupCommand(t *testing.T) {
	p := &irm.IRMProvider{}
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
