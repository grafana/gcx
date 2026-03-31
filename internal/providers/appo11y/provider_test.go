package appo11y_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers"
	_ "github.com/grafana/gcx/internal/providers/appo11y" // triggers init() self-registration
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
}
