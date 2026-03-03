package root_test

import (
	"testing"

	"github.com/grafana/grafanactl/cmd/grafanactl/root"
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a minimal Provider implementation for root-level tests.
type mockProvider struct {
	name     string
	commands []*cobra.Command
}

func (m *mockProvider) Name() string                       { return m.name }
func (m *mockProvider) ShortDesc() string                  { return m.name + " provider" }
func (m *mockProvider) Commands() []*cobra.Command         { return m.commands }
func (m *mockProvider) Validate(_ map[string]string) error { return nil }
func (m *mockProvider) ConfigKeys() []string               { return nil }

var _ providers.Provider = (*mockProvider)(nil)

// findSubcommand returns the direct child of root whose Use equals name, or nil.
func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Use == name {
			return sub
		}
	}
	return nil
}

func TestNewCommand_ProvidersSubcommandAlwaysRegistered(t *testing.T) {
	// The "providers" meta-command must be present regardless of the provider list.
	for _, pp := range [][]providers.Provider{nil, {}} {
		rootCmd := root.NewCommandForTest("v0.0.0-test", pp)
		require.NotNil(t, findSubcommand(rootCmd, "providers"), "expected 'providers' subcommand to be registered")
	}
}

func TestNewCommand_ProviderCommandsRegistered(t *testing.T) {
	sub := &cobra.Command{Use: "slo"}
	pp := []providers.Provider{
		&mockProvider{name: "slo", commands: []*cobra.Command{sub}},
	}

	rootCmd := root.NewCommandForTest("v0.0.0-test", pp)

	assert.NotNil(t, findSubcommand(rootCmd, "slo"), "expected provider subcommand 'slo' to be registered")
}

func TestNewCommand_NilProviderSkipped(t *testing.T) {
	// A nil entry in the provider slice must not panic and must be ignored.
	require.NotPanics(t, func() {
		rootCmd := root.NewCommandForTest("v0.0.0-test", []providers.Provider{nil})
		// The nil entry contributes no subcommands.
		assert.Nil(t, findSubcommand(rootCmd, ""), "nil provider must not register any command")
	})
}

func TestNewCommand_MultipleProviders(t *testing.T) {
	sloCmd := &cobra.Command{Use: "slo"}
	oncallCmd := &cobra.Command{Use: "oncall"}
	pp := []providers.Provider{
		&mockProvider{name: "slo", commands: []*cobra.Command{sloCmd}},
		&mockProvider{name: "oncall", commands: []*cobra.Command{oncallCmd}},
	}

	rootCmd := root.NewCommandForTest("v0.0.0-test", pp)

	assert.NotNil(t, findSubcommand(rootCmd, "slo"), "expected 'slo' subcommand")
	assert.NotNil(t, findSubcommand(rootCmd, "oncall"), "expected 'oncall' subcommand")
}
