package appo11y_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	appo11y "github.com/grafana/gcx/internal/providers/appo11y"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: AppO11yProvider implements framework.Setupable.
var _ framework.Setupable = (*appo11y.AppO11yProvider)(nil)

func TestAppO11yProvider_RegisteredInRegistry(t *testing.T) {
	all := providers.All()
	var found providers.Provider
	for _, p := range all {
		if p.Name() == "appo11y" {
			found = p
			break
		}
	}
	require.NotNil(t, found, "expected provider 'appo11y' to be registered")
}

func TestAppO11yProvider_TypedRegistrations(t *testing.T) {
	all := providers.All()
	var found providers.Provider
	for _, p := range all {
		if p.Name() == "appo11y" {
			found = p
			break
		}
	}
	require.NotNil(t, found)

	regs := found.TypedRegistrations()
	require.Len(t, regs, 2, "expected 2 registrations: Overrides and Settings")

	for i, reg := range regs {
		assert.NotNil(t, reg.Schema, "registration[%d] Schema should not be nil", i)
		assert.NotNil(t, reg.Example, "registration[%d] Example should not be nil", i)
		assert.NotNil(t, reg.Factory, "registration[%d] Factory should not be nil", i)
	}

	// Verify GVK kinds
	kinds := make(map[string]bool)
	for _, reg := range regs {
		kinds[reg.GVK.Kind] = true
	}
	assert.True(t, kinds["Overrides"], "expected Overrides GVK")
	assert.True(t, kinds["Settings"], "expected Settings GVK")
}

func TestAppO11yProvider_Commands(t *testing.T) {
	all := providers.All()
	var found providers.Provider
	for _, p := range all {
		if p.Name() == "appo11y" {
			found = p
			break
		}
	}
	require.NotNil(t, found)

	cmds := found.Commands()
	require.Len(t, cmds, 1)
	assert.Equal(t, "appo11y", cmds[0].Use)

	// Verify subcommands exist
	subNames := make(map[string]bool)
	for _, sub := range cmds[0].Commands() {
		subNames[sub.Name()] = true
	}
	assert.True(t, subNames["overrides"], "expected 'overrides' subcommand")
	assert.True(t, subNames["settings"], "expected 'settings' subcommand")
	assert.True(t, subNames["setup"], "expected 'setup' subcommand")
}

func TestAppO11yProvider_ProductName(t *testing.T) {
	p := &appo11y.AppO11yProvider{}
	assert.Equal(t, "appo11y", p.ProductName())
}

func TestAppO11yProvider_Status(t *testing.T) {
	p := &appo11y.AppO11yProvider{}
	status, err := p.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "appo11y", status.Product)
	// No config keys → StateActive.
	assert.Equal(t, framework.StateActive, status.State)
}

func TestAppO11yProvider_InfraCategories(t *testing.T) {
	p := &appo11y.AppO11yProvider{}
	assert.Nil(t, p.InfraCategories())
}

func TestAppO11yProvider_ResolveChoices(t *testing.T) {
	p := &appo11y.AppO11yProvider{}
	choices, err := p.ResolveChoices(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, choices)
}

func TestAppO11yProvider_ValidateSetup(t *testing.T) {
	p := &appo11y.AppO11yProvider{}
	assert.NoError(t, p.ValidateSetup(context.Background(), nil))
}

func TestAppO11yProvider_Setup(t *testing.T) {
	p := &appo11y.AppO11yProvider{}
	err := p.Setup(context.Background(), nil)
	assert.True(t, errors.Is(err, framework.ErrSetupNotSupported))
}

func TestAppO11yProvider_SetupCommand(t *testing.T) {
	p := &appo11y.AppO11yProvider{}
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

	assert.True(t, errors.Is(err, framework.ErrSetupNotSupported), "expected ErrSetupNotSupported, got %v", err)
	assert.NotEmpty(t, stderr.String(), "expected message written to stderr")
}
