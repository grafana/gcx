package root_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a minimal Provider implementation for root-level tests.
type mockProvider struct {
	name     string
	commands []*cobra.Command
}

func (m *mockProvider) Name() string                               { return m.name }
func (m *mockProvider) ShortDesc() string                          { return m.name + " provider" }
func (m *mockProvider) Commands() []*cobra.Command                 { return m.commands }
func (m *mockProvider) Validate(_ map[string]string) error         { return nil }
func (m *mockProvider) ConfigKeys() []providers.ConfigKey          { return nil }
func (m *mockProvider) TypedRegistrations() []adapter.Registration { return nil }

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

func TestNewCommand_DefaultHelpAndCompletionRegistered(t *testing.T) {
	rootCmd := root.NewCommandForTest("v0.0.0-test", nil)

	var foundHelp, foundCompletion bool
	for _, sub := range rootCmd.Commands() {
		switch sub.Name() {
		case "help":
			foundHelp = true
		case "completion":
			foundCompletion = true
		}
	}

	assert.True(t, foundHelp, "expected 'help' command to be registered")
	assert.True(t, foundCompletion, "expected 'completion' command to be registered")
}

func TestValidateArgs_GroupCommandRejectsUnexpectedArgs(t *testing.T) {
	agentsCmd := &cobra.Command{Use: "agents", Short: "Query agents.", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	conversationsCmd := &cobra.Command{Use: "conversations", Short: "Query conversations.", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	sigilCmd := &cobra.Command{Use: "sigil", Short: "Manage Sigil."}
	sigilCmd.AddCommand(agentsCmd, conversationsCmd)

	rootCmd := root.NewCommandForTest("v0.0.0-test", []providers.Provider{
		&mockProvider{name: "sigil", commands: []*cobra.Command{sigilCmd}},
	})

	err := root.ValidateArgs(rootCmd, []string{"sigil", "--context", "dev", "show"})
	require.Error(t, err)
	require.ErrorContains(t, err, `unknown command "show" for "gcx sigil"`)
	require.ErrorContains(t, err, "Usage:")
	require.ErrorContains(t, err, "Available Commands:")
	require.ErrorContains(t, err, "agents")
	require.ErrorContains(t, err, "conversations")
}

func TestValidateArgs_GroupCommandRejectsUnexpectedArgsWithLeadingRootFlags(t *testing.T) {
	agentsCmd := &cobra.Command{Use: "agents", Short: "Query agents.", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	sigilCmd := &cobra.Command{Use: "sigil", Short: "Manage Sigil."}
	sigilCmd.AddCommand(agentsCmd)

	rootCmd := root.NewCommandForTest("v0.0.0-test", []providers.Provider{
		&mockProvider{name: "sigil", commands: []*cobra.Command{sigilCmd}},
	})

	err := root.ValidateArgs(rootCmd, []string{"--agent", "--context", "dev", "sigil", "show"})
	require.Error(t, err)
	require.ErrorContains(t, err, `unknown command "show" for "gcx sigil"`)
	require.ErrorContains(t, err, "Usage:")
	require.ErrorContains(t, err, "Available Commands:")
	require.ErrorContains(t, err, "agents")
}

func TestValidateArgs_NestedGroupRejectsUnexpectedArgs(t *testing.T) {
	showCmd := &cobra.Command{Use: "show", Short: "Show agents.", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	versionsCmd := &cobra.Command{Use: "versions", Short: "List versions.", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	agentsCmd := &cobra.Command{Use: "agents", Short: "Query agents."}
	agentsCmd.PersistentFlags().Int("limit", 0, "Limit")
	agentsCmd.AddCommand(showCmd, versionsCmd)

	sigilCmd := &cobra.Command{Use: "sigil", Short: "Manage Sigil."}
	sigilCmd.AddCommand(agentsCmd)

	rootCmd := root.NewCommandForTest("v0.0.0-test", []providers.Provider{
		&mockProvider{name: "sigil", commands: []*cobra.Command{sigilCmd}},
	})

	err := root.ValidateArgs(rootCmd, []string{"sigil", "agents", "--limit", "10", "foo"})
	require.Error(t, err)
	require.ErrorContains(t, err, `unknown command "foo" for "gcx sigil agents"`)
	require.ErrorContains(t, err, "Usage:")
	require.ErrorContains(t, err, "Available Commands:")
	require.ErrorContains(t, err, "show")
	require.ErrorContains(t, err, "versions")
}

func TestValidateArgs_AllowsHelpAndCompletionCommands(t *testing.T) {
	rootCmd := root.NewCommandForTest("v0.0.0-test", nil)

	assert.NoError(t, root.ValidateArgs(rootCmd, []string{"help"}))
	assert.NoError(t, root.ValidateArgs(rootCmd, []string{"help", "sigil"}))
	assert.NoError(t, root.ValidateArgs(rootCmd, []string{"completion", "bash"}))
}
