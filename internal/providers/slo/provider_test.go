package slo_test

import (
	"testing"

	"github.com/grafana/gcx/internal/providers/slo"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSLOProvider_Interface(t *testing.T) {
	p := &slo.SLOProvider{}

	assert.Equal(t, "slo", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.NoError(t, p.Validate(nil))
	assert.NoError(t, p.Validate(map[string]string{}))
	assert.Nil(t, p.ConfigKeys())
}

func TestSLOProvider_Commands(t *testing.T) {
	p := &slo.SLOProvider{}
	cmds := p.Commands()
	require.Len(t, cmds, 1)

	sloCmd := cmds[0]
	assert.Equal(t, "slo", sloCmd.Use)

	// Find definitions subcommand
	var defsCmd *cobra.Command
	for _, sub := range sloCmd.Commands() {
		if sub.Name() == "definitions" {
			defsCmd = sub
			break
		}
	}
	require.NotNil(t, defsCmd, "expected 'definitions' subcommand")

	// Check all expected subcommands exist under definitions
	subNames := make([]string, 0, len(defsCmd.Commands()))
	for _, sub := range defsCmd.Commands() {
		subNames = append(subNames, sub.Name())
	}
	assert.Contains(t, subNames, "list")
	assert.Contains(t, subNames, "get")
	assert.Contains(t, subNames, "push")
	assert.Contains(t, subNames, "pull")
	assert.Contains(t, subNames, "delete")

	// Find reports subcommand
	var reportsCmd *cobra.Command
	for _, sub := range sloCmd.Commands() {
		if sub.Name() == "reports" {
			reportsCmd = sub
			break
		}
	}
	require.NotNil(t, reportsCmd, "expected 'reports' subcommand")

	// Check all expected subcommands exist under reports
	reportSubNames := make([]string, 0, len(reportsCmd.Commands()))
	for _, sub := range reportsCmd.Commands() {
		reportSubNames = append(reportSubNames, sub.Name())
	}
	assert.Contains(t, reportSubNames, "list")
	assert.Contains(t, reportSubNames, "get")
	assert.Contains(t, reportSubNames, "push")
	assert.Contains(t, reportSubNames, "pull")
	assert.Contains(t, reportSubNames, "delete")
}
