package root_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAgentsTestRoot(t *testing.T) *cobra.Command {
	t.Helper()

	showCmd := &cobra.Command{
		Use:         "show",
		Short:       "Show agents.",
		Annotations: map[string]string{agent.AnnotationLLMHint: "Use to inspect a single agent."},
		RunE:        func(_ *cobra.Command, _ []string) error { return nil },
	}
	versionsCmd := &cobra.Command{
		Use:     "versions",
		Aliases: []string{"revisions"},
		Short:   "List versions.",
		RunE:    func(_ *cobra.Command, _ []string) error { return nil },
	}
	agentsCmd := &cobra.Command{Use: "agents", Short: "Query agents."}
	agentsCmd.AddCommand(showCmd, versionsCmd)

	aio11yCmd := &cobra.Command{Use: "aio11y", Short: "Manage AI Observability."}
	aio11yCmd.AddCommand(agentsCmd)

	return root.NewCommandForTest("v0.0.0-test", []providers.Provider{
		&mockProvider{name: "aio11y", commands: []*cobra.Command{aio11yCmd}},
	})
}

func asUsageError(t *testing.T, err error) *fail.UsageError {
	t.Helper()
	usageErr := &fail.UsageError{}
	require.ErrorAs(t, err, &usageErr)
	return usageErr
}

func TestValidateArgs_UnknownSubcommandSuggestsClosestMatch(t *testing.T) {
	rootCmd := newAgentsTestRoot(t)

	err := root.ValidateArgs(rootCmd, []string{"aio11y", "agents", "shwo"})
	require.Error(t, err)
	require.ErrorContains(t, err, `unknown command "shwo" for "gcx aio11y agents"`)
	require.ErrorContains(t, err, "Did you mean this?")
	require.ErrorContains(t, err, "show")

	usageErr := asUsageError(t, err)
	assert.Contains(t, usageErr.Suggestions, "Did you mean 'gcx aio11y agents show'?")
	assert.Contains(t, usageErr.Suggestions, "Run 'gcx aio11y agents --help' for full usage and examples")
	require.Len(t, usageErr.Corrections, 1)
	assert.Equal(t, gcxerrors.Correction{
		Command: "gcx aio11y agents show",
		Hint:    "Use to inspect a single agent.",
	}, usageErr.Corrections[0])
}

func TestValidateArgs_CorrectionReattachesTrailingArgs(t *testing.T) {
	rootCmd := newAgentsTestRoot(t)

	err := root.ValidateArgs(rootCmd, []string{"aio11y", "agents", "shwo", "my-agent"})
	require.Error(t, err)

	usageErr := asUsageError(t, err)
	require.Len(t, usageErr.Corrections, 1)
	assert.Equal(t, "gcx aio11y agents show my-agent", usageErr.Corrections[0].Command)
}

func TestValidateArgs_SuggestsViaAlias(t *testing.T) {
	rootCmd := newAgentsTestRoot(t)

	err := root.ValidateArgs(rootCmd, []string{"aio11y", "agents", "revisons"})
	require.Error(t, err)

	usageErr := asUsageError(t, err)
	require.Len(t, usageErr.Corrections, 1)
	// The alias matched, but the correction uses the canonical name.
	assert.Equal(t, "gcx aio11y agents versions", usageErr.Corrections[0].Command)
}

func TestValidateArgs_NoMatchKeepsGenericError(t *testing.T) {
	rootCmd := newAgentsTestRoot(t)

	err := root.ValidateArgs(rootCmd, []string{"aio11y", "agents", "kubectl"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "Did you mean this?")
	require.ErrorContains(t, err, "Available Commands:")

	usageErr := asUsageError(t, err)
	assert.Empty(t, usageErr.Corrections)
	assert.Equal(t, []string{"Run 'gcx aio11y agents --help' for full usage and examples"}, usageErr.Suggestions)
}
