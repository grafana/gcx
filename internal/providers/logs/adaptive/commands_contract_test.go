package logs_test

// These tests pin the pre-GA agent output contract for the adaptive logs
// command family: in agent mode a finite command emits EXACTLY ONE JSON value
// on stdout, default human stdout stays byte-identical (the delete commands
// keep their historical styled confirmation line, now rendered by the text
// codec), the create/update confirmation prose moved to stderr so stdout is a
// single parseable document, and explicit -o json/yaml always wins.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	logs "github.com/grafana/gcx/internal/providers/logs/adaptive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newContractLoader starts one fake server answering both GCOM stack
// discovery and the Adaptive Logs API (via handler), and returns a
// ConfigLoader wired to it through a temp config file so the real command
// tree runs end-to-end.
func newContractLoader(t *testing.T, handler http.HandlerFunc) *providers.ConfigLoader {
	t.Helper()

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/api/instances/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":1,"hmInstancePromId":42,"hmInstancePromUrl":%q,"hlInstanceId":42,"hlInstanceUrl":%q,"htInstanceId":42,"htInstanceUrl":%q}`,
			srv.URL, srv.URL, srv.URL)
	})
	mux.HandleFunc("/", handler)

	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	cfg := fmt.Sprintf("contexts:\n  default:\n    cloud:\n      token: test-token\n      stack: teststack\n      api-url: %s\ncurrent-context: default\n", srv.URL)
	require.NoError(t, os.WriteFile(cfgPath, []byte(cfg), 0o600))

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgPath)
	return loader
}

type cmdResult struct {
	stdout string
	stderr string
	err    error
}

// runAdaptiveCmd executes the real adaptive logs command tree with the given
// agent-mode state and optional stdin. Agent mode must be set before the
// tree is built because output defaults are resolved at flag-binding time.
func runAdaptiveCmd(t *testing.T, loader *providers.ConfigLoader, agentMode bool, stdin string, args ...string) cmdResult {
	t.Helper()

	prior := agent.IsAgentMode()
	agent.SetFlag(agentMode)
	t.Cleanup(func() { agent.SetFlag(prior) })

	root := logs.Commands(loader)
	// Mirror the production root (cmd/gcx/root): errors and usage are
	// reported by the caller, never printed into the output streams.
	root.SilenceUsage = true
	root.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.Execute()
	return cmdResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// decodeOneJSONValue asserts stdout carries exactly one JSON value followed
// by EOF, and returns the decoded value.
func decodeOneJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout is not valid JSON: %q", stdout)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF, "stdout must contain exactly one JSON value: %q", stdout)
	return first
}

// fakeLogsAPI serves the Adaptive Logs endpoints exercised by the delete,
// create, and update commands.
func fakeLogsAPI() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/adaptive-logs/exemptions/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/adaptive-logs/segment" && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(r.URL.Path, "/adaptive-logs/drop-rules/") && r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/adaptive-logs/drop-rules" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":"dr1","segment_id":"__global__","version":1,"name":"my rule","body":{"drop_rate":0.5,"stream_selector":"{app=\"x\"}","levels":["debug"]}}`)
		case strings.HasPrefix(r.URL.Path, "/adaptive-logs/drop-rules/") && r.Method == http.MethodPut:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"dr1","segment_id":"__global__","version":1,"name":"my rule","body":{"drop_rate":0.7,"stream_selector":"{app=\"x\"}","levels":["debug"]}}`)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestLogsAdaptiveDeletes_OutputContract(t *testing.T) {
	deletes := []struct {
		name       string
		args       []string
		id         string
		wantKind   string
		wantStdout string // exact human default stdout (byte-identical)
	}{
		{
			name:       "exemptions delete",
			args:       []string{"exemptions", "delete", "e1"},
			id:         "e1",
			wantKind:   "exemption",
			wantStdout: "✔ Deleted exemption \"e1\"\n",
		},
		{
			name:       "segments delete",
			args:       []string{"segments", "delete", "s1"},
			id:         "s1",
			wantKind:   "segment",
			wantStdout: "✔ Deleted segment \"s1\"\n",
		},
		{
			name:       "drop-rules delete",
			args:       []string{"drop-rules", "delete", "dr1"},
			id:         "dr1",
			wantKind:   "drop-rule",
			wantStdout: "✔ Deleted drop rule \"dr1\"\n",
		},
	}

	for _, tc := range deletes {
		t.Run(tc.name, func(t *testing.T) {
			forceArgs := append(append([]string{}, tc.args...), "--force")

			t.Run("human default stdout is byte-identical confirmation line", func(t *testing.T) {
				loader := newContractLoader(t, fakeLogsAPI())
				res := runAdaptiveCmd(t, loader, false, "", forceArgs...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				assert.Equal(t, tc.wantStdout, res.stdout)
			})

			t.Run("agent mode emits one mutation doc", func(t *testing.T) {
				loader := newContractLoader(t, fakeLogsAPI())
				res := runAdaptiveCmd(t, loader, true, "", forceArgs...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)

				doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
				require.True(t, ok, "stdout document must be an object")
				assert.Equal(t, "gcx.mutation", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, "deleted", doc["action"])
				assert.Equal(t, true, doc["changed"])
				target, ok := doc["target"].(map[string]any)
				require.True(t, ok, "target must be an object")
				assert.Equal(t, tc.wantKind, target["kind"])
				assert.Equal(t, tc.id, target["id"])
			})

			t.Run("explicit -o json wins", func(t *testing.T) {
				loader := newContractLoader(t, fakeLogsAPI())
				res := runAdaptiveCmd(t, loader, false, "", append(append([]string{}, forceArgs...), "-o", "json")...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
				require.True(t, ok, "stdout document must be an object")
				assert.Equal(t, "gcx.mutation", doc["type"])
			})

			t.Run("agent mode without --force is rejected before mutation", func(t *testing.T) {
				loader := newContractLoader(t, fakeLogsAPI())
				res := runAdaptiveCmd(t, loader, true, "", tc.args...)
				require.ErrorIs(t, res.err, providers.ErrAgentModeRequiresForce)
				assert.Empty(t, res.stdout)
			})

			t.Run("interactive decline deletes nothing and prints nothing", func(t *testing.T) {
				loader := newContractLoader(t, fakeLogsAPI())
				res := runAdaptiveCmd(t, loader, false, "n\n", tc.args...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				assert.Empty(t, res.stdout)
				assert.Contains(t, res.stderr, "Aborted.")
			})
		})
	}
}

func TestLogsDropRulesCreateUpdate_StdoutSingleDoc(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "rule.json")
	require.NoError(t, os.WriteFile(specPath,
		[]byte(`{"version":1,"name":"my rule","body":{"drop_rate":0.5,"stream_selector":"{app=\"x\"}","levels":["debug"]}}`), 0o600))

	verbs := []struct {
		name         string
		args         []string
		wantStderrIn string
	}{
		{
			name:         "create",
			args:         []string{"drop-rules", "create", "-f", specPath},
			wantStderrIn: `Created drop rule "my rule" (id=dr1)`,
		},
		{
			name:         "update",
			args:         []string{"drop-rules", "update", "dr1", "-f", specPath},
			wantStderrIn: `Updated drop rule "my rule" (id=dr1)`,
		},
	}

	for _, tc := range verbs {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("human default stdout is a single JSON doc with prose on stderr", func(t *testing.T) {
				loader := newContractLoader(t, fakeLogsAPI())
				res := runAdaptiveCmd(t, loader, false, "", tc.args...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)

				doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
				require.True(t, ok, "stdout document must be an object")
				assert.Equal(t, "dr1", doc["id"])
				// The confirmation prose is a diagnostic on stderr, not part
				// of the stdout document.
				assert.Contains(t, res.stderr, tc.wantStderrIn)
				assert.NotContains(t, res.stdout, "drop rule \"my rule\"")
			})

			t.Run("agent mode emits exactly one JSON doc", func(t *testing.T) {
				loader := newContractLoader(t, fakeLogsAPI())
				res := runAdaptiveCmd(t, loader, true, "", tc.args...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
				require.True(t, ok, "stdout document must be an object")
				assert.Equal(t, "dr1", doc["id"])
			})

			t.Run("explicit -o yaml wins", func(t *testing.T) {
				loader := newContractLoader(t, fakeLogsAPI())
				res := runAdaptiveCmd(t, loader, false, "", append(append([]string{}, tc.args...), "-o", "yaml")...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				assert.Contains(t, res.stdout, "id: dr1")
				assert.Contains(t, res.stderr, tc.wantStderrIn)
			})
		})
	}
}
