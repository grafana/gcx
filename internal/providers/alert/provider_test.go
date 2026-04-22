package alert_test

import (
	"context"
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: AlertProvider satisfies StatusDetectable.
var _ framework.StatusDetectable = (*alert.AlertProvider)(nil)

func TestAlertProvider_Interface(t *testing.T) {
	p := &alert.AlertProvider{}

	assert.Equal(t, "alert", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	require.NoError(t, p.Validate(nil))
	assert.Nil(t, p.ConfigKeys())
}

func TestAlertProvider_StatusDetectable(t *testing.T) {
	p := &alert.AlertProvider{}

	t.Run("NotSetupable", func(t *testing.T) {
		_, ok := any(p).(framework.Setupable)
		assert.False(t, ok, "alert provider must not implement Setupable")
	})

	t.Run("ProductName", func(t *testing.T) {
		assert.Equal(t, p.Name(), p.ProductName())
	})

	t.Run("Status", func(t *testing.T) {
		// Alert has no ConfigKeys → ConfigKeysStatus returns StateActive.
		status, err := p.Status(context.Background())
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.NotEmpty(t, string(status.State))
		validStates := map[framework.ProductState]bool{
			framework.StateNotConfigured: true,
			framework.StateConfigured:    true,
			framework.StateActive:        true,
			framework.StateError:         true,
		}
		assert.True(t, validStates[status.State], "unexpected state: %q", status.State)
	})
}

func TestAlertProvider_Commands(t *testing.T) {
	p := &alert.AlertProvider{}
	cmds := p.Commands()

	require.Len(t, cmds, 1)
	alertCmd := cmds[0]
	assert.Equal(t, "alert", alertCmd.Use)

	// Collect subcommand names.
	subNames := make(map[string]bool)
	for _, sub := range alertCmd.Commands() {
		subNames[sub.Use] = true
	}
	assert.True(t, subNames["rules"], "alert should have rules subcommand")
	assert.True(t, subNames["groups"], "alert should have groups subcommand")
	assert.True(t, subNames["instances"], "alert should have instances subcommand")

	// Verify rules sub-commands.
	var rulesCmd *cobra.Command
	for _, sub := range alertCmd.Commands() {
		if sub.Use == "rules" {
			rulesCmd = sub
			break
		}
	}
	require.NotNil(t, rulesCmd, "rules command should exist")

	rulesSubNames := make(map[string]bool)
	for _, sub := range rulesCmd.Commands() {
		rulesSubNames[sub.Use] = true
	}
	assert.True(t, rulesSubNames["list"], "rules should have list subcommand")
	assert.True(t, rulesSubNames["get UID"], "rules should have get subcommand")
	assert.False(t, rulesSubNames["status [UID]"], "rules should not have status subcommand (merged into list --wide)")
}
