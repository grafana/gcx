package datasources_test

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
	"github.com/grafana/gcx/cmd/gcx/datasources"
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

// TestQuerySubcommandUse verifies the query constructor sets Use="query ...".
func TestQuerySubcommandUse(t *testing.T) {
	cmd := datasources.QueryCmd(newConfigOpts())
	assert.Equal(t, "query", cmd.Name())
}

func TestSinceValidationOnQueryCommand(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		expectErr string
	}{
		{
			name:      "since+from rejected",
			args:      []string{"query", "uid", "expr", "--since", "1h", "--from", "now-2h"},
			expectErr: "--since is mutually exclusive with --from",
		},
		{
			name:      "zero since rejected",
			args:      []string{"query", "uid", "expr", "--since", "0"},
			expectErr: "--since must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := datasources.QueryCmd(newConfigOpts())
			err := executeQueryCommand(t, cmd, tt.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectErr)
		})
	}
}

func TestSinceResolvesRelativeRangeOnQueryCommand(t *testing.T) {
	var capturedPath string
	var capturedBody map[string]any
	server := newQueryCaptureServer(t, "loki", func(path string, body map[string]any) {
		capturedPath = path
		capturedBody = body
	})
	defer server.Close()

	configOpts := newConfigOptsWithServer(t, server.URL)
	cmd := datasources.QueryCmd(configOpts)

	referenceNow := time.Now()
	err := executeQueryCommand(t, cmd, []string{"query", "uid", `{job="x"}`, "--since", "1h", "--to", "now-6h", "-o", "json"})
	require.NoError(t, err)
	require.NotEmpty(t, capturedPath)
	require.NotNil(t, capturedBody)

	start := parseUnixMillisField(t, capturedBody, "from")
	end := parseUnixMillisField(t, capturedBody, "to")

	assert.WithinDuration(t, end.Add(-time.Hour), start, time.Second)
	assert.WithinDuration(t, referenceNow.Add(-6*time.Hour), end, 5*time.Second)
}

// TestQueryRequiresBothArgs verifies that query requires exactly 2 positional args.
func TestQueryRequiresBothArgs(t *testing.T) {
	err := executeQueryCommand(t, datasources.QueryCmd(newConfigOpts()), []string{"query"})
	require.Error(t, err)
}
