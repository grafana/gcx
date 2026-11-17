package metrics_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
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
// datasources/prometheus commands' Example text and LLM hint were
// overridden to say "gcx metrics search <name>", not the original
// "gcx datasources prometheus search-<name>" — otherwise a caller copying
// the example gets the wrong command.
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
		assert.Contains(t, cmd.Annotations[agent.AnnotationLLMHint], "gcx metrics search "+name)
	}
}

// TestSearchCommands_Experimental proves the group and every leaf are
// marked experimental, and that rewriting the leaves' LLM hint keeps the
// annotations the datasource commands set.
func TestSearchCommands_Experimental(t *testing.T) {
	group := metrics.SearchCommands(nil)
	assert.Equal(t, agent.StabilityExperimental, group.Annotations[agent.AnnotationStability])

	for _, sub := range group.Commands() {
		assert.Equal(t, agent.StabilityExperimental, sub.Annotations[agent.AnnotationStability], "%q must stay experimental", sub.Name())
		assert.Equal(t, "small", sub.Annotations[agent.AnnotationTokenCost], "%q must keep its token cost", sub.Name())
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

// TestSearchCommands_RequireTerm proves each command rejects a missing
// TERM/scope before any config is loaded or request made. It binds a real
// (unreachable-by-design) config rather than passing a nil loader: a nil
// *providers.ConfigLoader is valid and falls through to layered config
// discovery (see ConfigLoader's doc comment), so an unbound test would pass
// today only because the argument-count/scope guard fires first — and would
// silently start depending on the machine's own Grafana config if that
// guard were ever removed. Asserting the specific error message closes the
// same gap: a coincidental error from reaching a live config would also
// satisfy a bare require.Error.
func TestSearchCommands_RequireTerm(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s: a missing TERM/scope must fail before any request is made", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f, err := os.CreateTemp(t.TempDir(), "gcx-metrics-search-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(`
contexts:
  default:
    grafana:
      server: "` + srv.URL + `"
      token: "test-token"
      org-id: 1
current-context: default
`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(f.Name())

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "metric-names", args: []string{"metric-names"}, wantErr: "requires at least 1 arg"},
		{name: "label-names", args: []string{"label-names"}, wantErr: "requires TERM, or a scope"},
		{name: "label-values missing LABEL", args: []string{"label-values"}, wantErr: "requires at least 1 arg"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := &cobra.Command{Use: "test"}
			root.AddCommand(metrics.SearchCommands(loader))
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(append([]string{"search"}, tc.args...))

			err := root.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}
