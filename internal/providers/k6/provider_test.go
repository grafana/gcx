package k6_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/grafana/gcx/internal/providers/k6"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: K6Provider implements framework.Setupable.
var _ framework.Setupable = (*k6.K6Provider)(nil)

func TestK6Provider_ProductName(t *testing.T) {
	p := &k6.K6Provider{}
	assert.Equal(t, "k6", p.ProductName())
}

func TestK6Provider_Status(t *testing.T) {
	p := &k6.K6Provider{}
	status, err := p.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "k6", status.Product)
	assert.NotEmpty(t, string(status.State))
}

func TestK6Provider_InfraCategories(t *testing.T) {
	p := &k6.K6Provider{}
	assert.Nil(t, p.InfraCategories())
}

func TestK6Provider_ResolveChoices(t *testing.T) {
	p := &k6.K6Provider{}
	choices, err := p.ResolveChoices(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, choices)
}

func TestK6Provider_ValidateSetup(t *testing.T) {
	p := &k6.K6Provider{}
	assert.NoError(t, p.ValidateSetup(context.Background(), nil))
}

func TestK6Provider_Setup(t *testing.T) {
	p := &k6.K6Provider{}
	err := p.Setup(context.Background(), nil)
	require.ErrorIs(t, err, framework.ErrSetupNotSupported)
}

func TestK6Provider_SetupCommand(t *testing.T) {
	p := &k6.K6Provider{}
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
