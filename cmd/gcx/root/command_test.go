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

// findSubcommand returns the direct child command whose name equals name, or nil.
func findSubcommand(cmd *cobra.Command, name string) *cobra.Command {
	for _, sub := range cmd.Commands() {
		if sub.Name() == name {
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

func TestNewCommand_TopLevelDatasourceCommandsRegistered(t *testing.T) {
	rootCmd := root.NewCommandForTest("v0.0.0-test", nil)

	for _, name := range []string{"prometheus", "loki", "pyroscope", "tempo"} {
		assert.NotNil(t, findSubcommand(rootCmd, name), "expected top-level datasource command %q", name)
	}
}

func TestNewCommand_DatasourcesCommandKeepsOnlyManagementAndGenericSubcommands(t *testing.T) {
	rootCmd := root.NewCommandForTest("v0.0.0-test", nil)
	datasourcesCmd := findSubcommand(rootCmd, "datasources")
	require.NotNil(t, datasourcesCmd, "expected 'datasources' subcommand")

	assert.NotNil(t, findSubcommand(datasourcesCmd, "list"), "expected 'datasources list' subcommand")
	assert.NotNil(t, findSubcommand(datasourcesCmd, "get"), "expected 'datasources get' subcommand")
	assert.NotNil(t, findSubcommand(datasourcesCmd, "generic"), "expected 'datasources generic' subcommand")

	for _, name := range []string{"prometheus", "loki", "pyroscope", "tempo"} {
		assert.Nil(t, findSubcommand(datasourcesCmd, name), "did not expect promoted datasource command %q under 'datasources'", name)
	}
}
