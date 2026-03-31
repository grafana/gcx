package query_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	dsquery "github.com/grafana/gcx/cmd/gcx/datasources/query"
	"github.com/grafana/gcx/internal/testutils"
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

func newConfigOptsWithServer(t *testing.T, serverURL string) *cmdconfig.Options {
	t.Helper()

	configFile := testutils.CreateTempFile(t, fmt.Sprintf(`current-context: test
contexts:
  test:
    grafana:
      server: %s
      token: test-token
      org-id: 1
`, serverURL))

	return &cmdconfig.Options{ConfigFile: configFile}
}

func executeQueryCommand(t *testing.T, cmd *cobra.Command, args []string) error {
	t.Helper()

	root := helperRoot(cmd)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)

	return root.Execute()
}

func newQueryCaptureServer(t *testing.T, datasourceType string, capture func(string, map[string]any)) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/bootdata":
			http.NotFound(w, r)
			return
		case r.Method == http.MethodGet && r.URL.Path == "/api/datasources/uid/uid":
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{
				"id":   1,
				"uid":  "uid",
				"name": "test",
				"type": datasourceType,
			}); err != nil {
				t.Errorf("encode datasource response: %v", err)
			}
			return
		case r.Method == http.MethodPost:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode query request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			capture(r.URL.Path, body)

			w.Header().Set("Content-Type", "application/json")
			switch {
			case strings.Contains(r.URL.Path, "/api/datasources/proxy/uid/"):
				_, _ = w.Write([]byte(`{"flamegraph":{"names":[],"levels":[],"total":"0","maxSelf":"0"}}`))
			case strings.Contains(r.URL.Path, "/query.grafana.app/") && datasourceType == "prometheus":
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"Time","type":"time"},{"name":"Value","type":"number","labels":{"job":"grafana"}}]},"data":{"values":[[1711893600000],[1]]}}]}}}`))
			case strings.Contains(r.URL.Path, "/query.grafana.app/"):
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
			default:
				t.Fatalf("unexpected query path: %s", r.URL.Path)
			}
			return
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
}

func parseUnixMillisField(t *testing.T, body map[string]any, key string) time.Time {
	t.Helper()

	raw, ok := body[key].(string)
	require.Truef(t, ok, "expected %q to be a string, got %T", key, body[key])

	ms, err := strconv.ParseInt(raw, 10, 64)
	require.NoError(t, err)

	return time.UnixMilli(ms)
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
			args:      []string{"query", "uid", `{job="x"}`, "--window", "1h", "--from", "now-2h"},
			expectErr: "--window is mutually exclusive with --from and --to",
		},
		{
			name:      "pyroscope: window+from rejected",
			cmd:       dsquery.PyroscopeCmd(newConfigOpts()),
			args:      []string{"query", "uid", `{service_name="x"}`, "--window", "1h", "--from", "now-2h", "--profile-type", "cpu"},
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
			err := executeQueryCommand(t, tt.cmd, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectErr)
		})
	}
}

func TestSinceValidationOnCommands(t *testing.T) {
	tests := []struct {
		name      string
		cmd       *cobra.Command
		args      []string
		expectErr string
	}{
		{
			name:      "prometheus: since+from rejected",
			cmd:       dsquery.PrometheusCmd(newConfigOpts()),
			args:      []string{"query", "uid", "up", "--since", "1h", "--from", "now-2h"},
			expectErr: "--since is mutually exclusive with --from",
		},
		{
			name:      "loki: since+from rejected",
			cmd:       dsquery.LokiCmd(newConfigOpts()),
			args:      []string{"query", "uid", `{job="x"}`, "--since", "1h", "--from", "now-2h"},
			expectErr: "--since is mutually exclusive with --from",
		},
		{
			name:      "pyroscope: since+from rejected",
			cmd:       dsquery.PyroscopeCmd(newConfigOpts()),
			args:      []string{"query", "uid", `{service_name="x"}`, "--since", "1h", "--from", "now-2h", "--profile-type", "cpu"},
			expectErr: "--since is mutually exclusive with --from",
		},
		{
			name:      "generic: since+from rejected",
			cmd:       dsquery.GenericCmd(newConfigOpts()),
			args:      []string{"query", "uid", "expr", "--since", "1h", "--from", "now-2h"},
			expectErr: "--since is mutually exclusive with --from",
		},
		{
			name:      "prometheus: since+window rejected",
			cmd:       dsquery.PrometheusCmd(newConfigOpts()),
			args:      []string{"query", "uid", "up", "--since", "1h", "--window", "1h"},
			expectErr: "--window and --since are mutually exclusive",
		},
		{
			name:      "loki: since+window rejected",
			cmd:       dsquery.LokiCmd(newConfigOpts()),
			args:      []string{"query", "uid", `{job="x"}`, "--since", "1h", "--window", "1h"},
			expectErr: "--window and --since are mutually exclusive",
		},
		{
			name:      "pyroscope: since+window rejected",
			cmd:       dsquery.PyroscopeCmd(newConfigOpts()),
			args:      []string{"query", "uid", `{service_name="x"}`, "--since", "1h", "--window", "1h", "--profile-type", "cpu"},
			expectErr: "--window and --since are mutually exclusive",
		},
		{
			name:      "generic: since+window rejected",
			cmd:       dsquery.GenericCmd(newConfigOpts()),
			args:      []string{"query", "uid", "expr", "--since", "1h", "--window", "1h"},
			expectErr: "--window and --since are mutually exclusive",
		},
		{
			name:      "loki: negative since rejected",
			cmd:       dsquery.LokiCmd(newConfigOpts()),
			args:      []string{"query", "uid", `{job="x"}`, "--since", "-1h"},
			expectErr: "--since must be greater than 0",
		},
		{
			name:      "generic: zero since rejected",
			cmd:       dsquery.GenericCmd(newConfigOpts()),
			args:      []string{"query", "uid", "expr", "--since", "0"},
			expectErr: "--since must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := executeQueryCommand(t, tt.cmd, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectErr)
		})
	}
}

func TestSinceResolvesRelativeRangeOnCommands(t *testing.T) {
	tests := []struct {
		name           string
		datasourceType string
		newCmd         func(*cmdconfig.Options) *cobra.Command
		args           []string
		startField     string
		endField       string
	}{
		{
			name:           "prometheus",
			datasourceType: "prometheus",
			newCmd:         dsquery.PrometheusCmd,
			args:           []string{"query", "uid", "up", "--since", "1h", "--to", "now-6h", "-o", "json"},
			startField:     "from",
			endField:       "to",
		},
		{
			name:           "loki",
			datasourceType: "loki",
			newCmd:         dsquery.LokiCmd,
			args:           []string{"query", "uid", `{job="x"}`, "--since", "1h", "--to", "now-6h", "-o", "json"},
			startField:     "from",
			endField:       "to",
		},
		{
			name:           "pyroscope",
			datasourceType: "grafana-pyroscope-datasource",
			newCmd:         dsquery.PyroscopeCmd,
			args:           []string{"query", "uid", `{service_name="x"}`, "--since", "1h", "--to", "now-6h", "--profile-type", "cpu", "-o", "json"},
			startField:     "start",
			endField:       "end",
		},
		{
			name:           "generic",
			datasourceType: "loki",
			newCmd:         dsquery.GenericCmd,
			args:           []string{"query", "uid", `{job="x"}`, "--since", "1h", "--to", "now-6h", "-o", "json"},
			startField:     "from",
			endField:       "to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedPath string
			var capturedBody map[string]any
			server := newQueryCaptureServer(t, tt.datasourceType, func(path string, body map[string]any) {
				capturedPath = path
				capturedBody = body
			})
			defer server.Close()

			configOpts := newConfigOptsWithServer(t, server.URL)
			cmd := tt.newCmd(configOpts)

			referenceNow := time.Now()
			err := executeQueryCommand(t, cmd, tt.args)
			require.NoError(t, err)
			require.NotEmpty(t, capturedPath)
			require.NotNil(t, capturedBody)

			start := parseUnixMillisField(t, capturedBody, tt.startField)
			end := parseUnixMillisField(t, capturedBody, tt.endField)

			assert.WithinDuration(t, end.Add(-time.Hour), start, time.Second)
			assert.WithinDuration(t, referenceNow.Add(-6*time.Hour), end, 5*time.Second)
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
			err := executeQueryCommand(t, tt.cmd, []string{"query", tt.forbiddenFlag, "value"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown flag")
		})
	}
}

// TestTempoReturnsNotImplemented verifies the tempo stub error message.
func TestTempoReturnsNotImplemented(t *testing.T) {
	err := executeQueryCommand(t, dsquery.TempoCmd(), []string{"query"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tempo queries are not yet implemented")
}

// TestGenericRequiresBothArgs verifies that generic query requires exactly 2 positional args.
func TestGenericRequiresBothArgs(t *testing.T) {
	err := executeQueryCommand(t, dsquery.GenericCmd(newConfigOpts()), []string{"query"})
	require.Error(t, err)
}
