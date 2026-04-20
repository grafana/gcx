package slo_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/grafana/gcx/internal/providers/slo"
	"github.com/grafana/gcx/internal/setup/framework"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time assertion: SLOProvider implements framework.Setupable.
var _ framework.Setupable = (*slo.SLOProvider)(nil)

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

func TestSLOProvider_ProductName(t *testing.T) {
	p := &slo.SLOProvider{}
	assert.Equal(t, "slo", p.ProductName())
}

func TestSLOProvider_Status(t *testing.T) {
	p := &slo.SLOProvider{}
	status, err := p.Status(context.Background())
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "slo", status.Product)
	// No config keys → StateActive.
	assert.Equal(t, framework.StateActive, status.State)
}

func TestSLOProvider_InfraCategories(t *testing.T) {
	p := &slo.SLOProvider{}
	assert.Nil(t, p.InfraCategories())
}

func TestSLOProvider_ResolveChoices(t *testing.T) {
	p := &slo.SLOProvider{}
	choices, err := p.ResolveChoices(context.Background(), "any")
	require.NoError(t, err)
	assert.Nil(t, choices)
}

func TestSLOProvider_ValidateSetup(t *testing.T) {
	p := &slo.SLOProvider{}
	assert.NoError(t, p.ValidateSetup(context.Background(), nil))
}

func TestSLOProvider_Setup(t *testing.T) {
	p := &slo.SLOProvider{}
	err := p.Setup(context.Background(), nil)
	assert.True(t, errors.Is(err, framework.ErrSetupNotSupported))
}

func TestSLOProvider_SetupCommand(t *testing.T) {
	p := &slo.SLOProvider{}
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
