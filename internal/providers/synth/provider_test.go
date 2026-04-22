package synth_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: SynthProvider implements framework.Setupable.
var _ framework.Setupable = (*synth.SynthProvider)(nil)

func TestSynthProvider_Interface(t *testing.T) {
	var p providers.Provider = &synth.SynthProvider{}
	assert.Equal(t, "synth", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.NotEmpty(t, p.Commands())
}

func TestSynthProvider_ConfigKeys(t *testing.T) {
	p := &synth.SynthProvider{}
	keys := p.ConfigKeys()
	require.Len(t, keys, 3)

	keyMap := make(map[string]providers.ConfigKey)
	for _, k := range keys {
		keyMap[k.Name] = k
	}

	smURL, ok := keyMap["sm-url"]
	require.True(t, ok)
	assert.False(t, smURL.Secret)

	smToken, ok := keyMap["sm-token"]
	require.True(t, ok)
	assert.True(t, smToken.Secret)

	smDS, ok := keyMap["sm-metrics-datasource-uid"]
	require.True(t, ok)
	assert.False(t, smDS.Secret)
}

func TestSynthProvider_Validate(t *testing.T) {
	p := &synth.SynthProvider{}

	// Validate always returns nil because both sm-url and sm-token
	// can be auto-discovered at runtime.
	tests := []struct {
		name string
		cfg  map[string]string
	}{
		{name: "both keys set", cfg: map[string]string{"sm-url": "https://example.com", "sm-token": "tok"}},
		{name: "only sm-url", cfg: map[string]string{"sm-url": "https://example.com"}},
		{name: "only sm-token", cfg: map[string]string{"sm-token": "tok"}},
		{name: "empty config", cfg: map[string]string{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, p.Validate(tc.cfg))
		})
	}
}

func TestSynthProvider_ProductName(t *testing.T) {
	p := &synth.SynthProvider{}
	assert.Equal(t, "synth", p.ProductName())
}

func TestSynthProvider_Status(t *testing.T) {
	p := &synth.SynthProvider{}
	status, err := p.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "synth", status.Product)
	assert.NotEmpty(t, string(status.State))
}

func TestSynthProvider_InfraCategories(t *testing.T) {
	p := &synth.SynthProvider{}
	assert.Nil(t, p.InfraCategories())
}

func TestSynthProvider_ResolveChoices(t *testing.T) {
	p := &synth.SynthProvider{}
	choices, err := p.ResolveChoices(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, choices)
}

func TestSynthProvider_ValidateSetup(t *testing.T) {
	p := &synth.SynthProvider{}
	assert.NoError(t, p.ValidateSetup(context.Background(), nil))
}

func TestSynthProvider_Setup(t *testing.T) {
	p := &synth.SynthProvider{}
	err := p.Setup(context.Background(), nil)
	require.ErrorIs(t, err, framework.ErrSetupNotSupported)
}

func TestSynthProvider_SetupCommand(t *testing.T) {
	p := &synth.SynthProvider{}
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
