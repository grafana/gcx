package slo_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/grafana/gcx/internal/providers/slo"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSLOProvider_Interface(t *testing.T) {
	p := slo.NewSLOProvider()

	assert.Equal(t, "slo", p.Name())
	assert.NotEmpty(t, p.ShortDesc())
	assert.NoError(t, p.Validate(nil))
	assert.NoError(t, p.Validate(map[string]string{}))
	assert.Nil(t, p.ConfigKeys())
}

func TestSLOProvider_Commands(t *testing.T) {
	p := slo.NewSLOProvider()
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

	var listCmd *cobra.Command
	for _, sub := range defsCmd.Commands() {
		if sub.Name() == "list" {
			listCmd = sub
			break
		}
	}
	require.NotNil(t, listCmd)
	limitFlag := listCmd.Flags().Lookup("limit")
	require.NotNil(t, limitFlag)
	assert.Equal(t, "0", limitFlag.DefValue, "list --limit should default to 0 (all SLOs); API returns the full list either way")

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

func TestSLOProvider_CommandsReturnFreshTrees(t *testing.T) {
	p := slo.NewSLOProvider()
	firstCommands := p.Commands()
	secondCommands := p.Commands()
	require.Len(t, firstCommands, 1)
	require.Len(t, secondCommands, 1)
	assert.NotSame(t, firstCommands[0], secondCommands[0], "each Commands call must return a fresh Cobra tree")

	firstRoot := &cobra.Command{Use: "first"}
	secondRoot := &cobra.Command{Use: "second"}
	var firstOutput, secondOutput bytes.Buffer
	firstRoot.SetOut(&firstOutput)
	secondRoot.SetOut(&secondOutput)
	firstRoot.AddCommand(firstCommands...)
	secondRoot.AddCommand(secondCommands...)

	definitions := firstCommands[0].Commands()[0]
	var list *cobra.Command
	for _, sub := range definitions.Commands() {
		if sub.Name() == "list" {
			list = sub
			break
		}
	}
	require.NotNil(t, list)
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprint(cmd.OutOrStdout(), "first tree")
		return err
	}

	firstRoot.SetArgs([]string{"slo", "definitions", "list"})
	require.NoError(t, firstRoot.Execute())
	assert.Equal(t, "first tree", firstOutput.String())
	assert.Empty(t, secondOutput.String())
}
