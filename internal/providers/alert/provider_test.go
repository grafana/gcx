package alert_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertProvider_Interface(t *testing.T) {
	p := &alert.AlertProvider{}

	assert.Equal(t, "alert", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	require.NoError(t, p.Validate(nil))
	assert.Nil(t, p.ConfigKeys())
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
