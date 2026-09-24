package metrics_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/providers/metrics"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchCommands_Structure(t *testing.T) {
	cmd := metrics.SearchCommands(nil)
	assert.Equal(t, "search", cmd.Name())

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.ElementsMatch(t, []string{"metric-names", "label-names", "label-values"}, names)
}

// TestSearchCommands_ExampleOverridesReferenceMetricsPath proves the reused
// datasources/prometheus commands' Example text was overridden to say
// "gcx metrics search <name>", not the original "gcx datasources prometheus
// search-<name>" — otherwise a caller copying the example gets the wrong
// command.
func TestSearchCommands_ExampleOverridesReferenceMetricsPath(t *testing.T) {
	byName := map[string]*cobra.Command{}
	for _, sub := range metrics.SearchCommands(nil).Commands() {
		byName[sub.Name()] = sub
	}

	for _, name := range []string{"metric-names", "label-names", "label-values"} {
		cmd, ok := byName[name]
		require.True(t, ok, "missing subcommand %q", name)
		assert.Contains(t, cmd.Example, "gcx metrics search "+name, "Example for %q should reference the metrics search path", name)
		assert.NotContains(t, cmd.Example, "datasources prometheus", "Example for %q should not reference the datasources path", name)
	}
}

// TestSearchCommands_MetricFlagPlacement proves --metric is wired
// through to label-names and label-values but not metric-names.
func TestSearchCommands_MetricFlagPlacement(t *testing.T) {
	byName := map[string]*cobra.Command{}
	for _, sub := range metrics.SearchCommands(nil).Commands() {
		byName[sub.Name()] = sub
	}

	assert.Nil(t, byName["metric-names"].Flags().Lookup("metric"))
	assert.NotNil(t, byName["label-names"].Flags().Lookup("metric"))
	assert.NotNil(t, byName["label-values"].Flags().Lookup("metric"))

	assert.Nil(t, byName["metric-names"].Flags().Lookup("metric-regex"))
	assert.NotNil(t, byName["label-names"].Flags().Lookup("metric-regex"))
	assert.NotNil(t, byName["label-values"].Flags().Lookup("metric-regex"))
}

func TestSearchCommands_RequireTerm(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "metric-names", args: []string{"metric-names"}},
		{name: "label-names", args: []string{"label-names"}},
		{name: "label-values missing LABEL", args: []string{"label-values"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := &cobra.Command{Use: "test"}
			root.AddCommand(metrics.SearchCommands(nil))
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(append([]string{"search"}, tc.args...))

			require.Error(t, root.Execute())
		})
	}
}
