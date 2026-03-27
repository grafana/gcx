package query_test

import (
	"bytes"
	"testing"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	dsquery "github.com/grafana/gcx/cmd/gcx/datasources/query"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperRoot creates a throw-away parent command so tests can call Execute()
// on a query subcommand without needing a live Grafana connection.
func helperRoot(sub *cobra.Command) *cobra.Command {
	root := &cobra.Command{Use: "test"}
	root.AddCommand(sub)
	return root
}

func newConfigOpts() *cmdconfig.Options {
	return &cmdconfig.Options{}
}

// TestQuerySubcommandUse verifies each exported constructor sets Use="query …".
func TestQuerySubcommandUse(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"prometheus", dsquery.PrometheusCmd(newConfigOpts())},
		{"loki", dsquery.LokiCmd(newConfigOpts())},
		{"pyroscope", dsquery.PyroscopeCmd(newConfigOpts())},
		{"tempo", dsquery.TempoCmd()},
		{"generic", dsquery.GenericCmd(newConfigOpts())},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, "query", tt.cmd.Name())
		})
	}
}

// TestWindowMutualExclusion verifies --window is mutually exclusive with --from/--to.
func TestWindowMutualExclusion(t *testing.T) {
	tests := []struct {
		name      string
		cmd       *cobra.Command
		args      []string
		expectErr string
	}{
		{
			name:      "prometheus: window+from rejected",
			cmd:       dsquery.PrometheusCmd(newConfigOpts()),
			args:      []string{"query", "uid", "up", "--window", "1h", "--from", "now-2h"},
			expectErr: "--window is mutually exclusive with --from and --to",
		},
		{
			name:      "prometheus: window+to rejected",
			cmd:       dsquery.PrometheusCmd(newConfigOpts()),
			args:      []string{"query", "uid", "up", "--window", "1h", "--to", "now"},
			expectErr: "--window is mutually exclusive with --from and --to",
		},
		{
			name:      "loki: window+from rejected",
			cmd:       dsquery.LokiCmd(newConfigOpts()),
			args:      []string{"query", "uid", "{job=\"x\"}", "--window", "1h", "--from", "now-2h"},
			expectErr: "--window is mutually exclusive with --from and --to",
		},
		{
			name:      "pyroscope: window+from rejected",
			cmd:       dsquery.PyroscopeCmd(newConfigOpts()),
			args:      []string{"query", "uid", "{service_name=\"x\"}", "--window", "1h", "--from", "now-2h", "--profile-type", "cpu"},
			expectErr: "--window is mutually exclusive with --from and --to",
		},
		{
			name:      "generic: window+from rejected",
			cmd:       dsquery.GenericCmd(newConfigOpts()),
			args:      []string{"query", "uid", "expr", "--window", "1h", "--from", "now-2h"},
			expectErr: "--window is mutually exclusive with --from and --to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := helperRoot(tt.cmd)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(tt.args)

			err := root.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectErr)
		})
	}
}

// TestNegativeConstraintFlags verifies that flags from other subcommands are NOT registered.
func TestNegativeConstraintFlags(t *testing.T) {
	tests := []struct {
		name          string
		cmd           *cobra.Command
		forbiddenFlag string
	}{
		{
			name:          "prometheus: no --profile-type",
			cmd:           dsquery.PrometheusCmd(newConfigOpts()),
			forbiddenFlag: "--profile-type",
		},
		{
			name:          "prometheus: no --limit",
			cmd:           dsquery.PrometheusCmd(newConfigOpts()),
			forbiddenFlag: "--limit",
		},
		{
			name:          "loki: no --profile-type",
			cmd:           dsquery.LokiCmd(newConfigOpts()),
			forbiddenFlag: "--profile-type",
		},
		{
			name:          "pyroscope: no --limit",
			cmd:           dsquery.PyroscopeCmd(newConfigOpts()),
			forbiddenFlag: "--limit",
		},
		{
			name:          "tempo: no --from",
			cmd:           dsquery.TempoCmd(),
			forbiddenFlag: "--from",
		},
		{
			name:          "tempo: no --to",
			cmd:           dsquery.TempoCmd(),
			forbiddenFlag: "--to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := helperRoot(tt.cmd)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs([]string{"query", tt.forbiddenFlag, "value"})

			err := root.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown flag")
		})
	}
}

// TestTempoReturnsNotImplemented verifies the tempo stub error message.
func TestTempoReturnsNotImplemented(t *testing.T) {
	root := helperRoot(dsquery.TempoCmd())
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"query"})

	err := root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tempo queries are not yet implemented")
}

// TestGenericRequiresBothArgs verifies that generic query requires exactly 2 positional args.
func TestGenericRequiresBothArgs(t *testing.T) {
	root := helperRoot(dsquery.GenericCmd(newConfigOpts()))
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"query"})

	err := root.Execute()
	require.Error(t, err)
}
