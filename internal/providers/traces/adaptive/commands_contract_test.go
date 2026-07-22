package traces_test

// These tests pin the pre-GA agent output contract for the adaptive traces
// command family: in agent mode a finite command emits EXACTLY ONE JSON value
// on stdout, partial failure of the multi-id policies delete returns an
// EmittedError carrying ExitPartialFailure after the batch document is on
// stdout, default human stdout stays byte-identical (empty — outcomes are
// stderr prose), and explicit -o json/yaml always wins.

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
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers"
	traces "github.com/grafana/gcx/internal/providers/traces/adaptive"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newContractLoader starts one fake server answering both GCOM stack
// discovery and the Adaptive Traces API (via handler), and returns a
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

// runAdaptiveCmd executes the real adaptive traces command tree with the
// given agent-mode state. Agent mode must be set before the tree is built
// because output defaults are resolved at flag-binding time.
func runAdaptiveCmd(t *testing.T, loader *providers.ConfigLoader, agentMode bool, args ...string) cmdResult {
	t.Helper()

	prior := agent.IsAgentMode()
	agent.SetFlag(agentMode)
	t.Cleanup(func() { agent.SetFlag(prior) })

	root := traces.Commands(loader)
	// Mirror the production root (cmd/gcx/root): errors and usage are
	// reported by the caller, never printed into the output streams.
	root.SilenceUsage = true
	root.SilenceErrors = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
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

// fakeTracesAPI serves the Adaptive Traces endpoints exercised by the
// recommendations apply/dismiss and policies delete commands. Policy IDs in
// failIDs return HTTP 500 on delete.
func fakeTracesAPI(failIDs ...string) http.HandlerFunc {
	fail := make(map[string]bool, len(failIDs))
	for _, id := range failIDs {
		fail[id] = true
	}
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/adaptive-traces/api/v1/recommendations/") &&
			(strings.HasSuffix(r.URL.Path, "/apply") || strings.HasSuffix(r.URL.Path, "/dismiss")):
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/adaptive-traces/api/v1/policies/") && r.Method == http.MethodDelete:
			id := strings.TrimPrefix(r.URL.Path, "/adaptive-traces/api/v1/policies/")
			if fail[id] {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"boom"}`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestTracesRecommendationsApplyDismiss_OutputContract(t *testing.T) {
	verbs := []struct {
		name         string
		verb         string
		wantAction   string
		wantStderrIn string
	}{
		{name: "apply", verb: "apply", wantAction: "applied", wantStderrIn: `Applied recommendation "r1"`},
		{name: "dismiss", verb: "dismiss", wantAction: "dismissed", wantStderrIn: `Dismissed recommendation "r1"`},
	}

	for _, tc := range verbs {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"recommendations", tc.verb, "r1", "--force"}

			t.Run("agent mode emits one mutation doc", func(t *testing.T) {
				loader := newContractLoader(t, fakeTracesAPI())
				res := runAdaptiveCmd(t, loader, true, args...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)

				doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
				require.True(t, ok, "stdout document must be an object")
				assert.Equal(t, "gcx.mutation", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, tc.wantAction, doc["action"])
				assert.Equal(t, true, doc["changed"])
				target, ok := doc["target"].(map[string]any)
				require.True(t, ok, "target must be an object")
				assert.Equal(t, "recommendation", target["kind"])
				assert.Equal(t, "r1", target["id"])
			})

			t.Run("human default stdout stays empty", func(t *testing.T) {
				loader := newContractLoader(t, fakeTracesAPI())
				res := runAdaptiveCmd(t, loader, false, args...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				assert.Empty(t, res.stdout, "default human stdout must stay byte-identical (empty)")
				assert.Contains(t, res.stderr, tc.wantStderrIn)
			})

			t.Run("agent dry-run emits one doc with dry_run", func(t *testing.T) {
				loader := newContractLoader(t, fakeTracesAPI())
				res := runAdaptiveCmd(t, loader, true, "recommendations", tc.verb, "r1", "--dry-run")
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
				require.True(t, ok, "stdout document must be an object")
				assert.Equal(t, true, doc["dry_run"])
				assert.Nil(t, doc["changed"], "dry-run must not claim a state change")
			})

			t.Run("human dry-run stdout stays empty", func(t *testing.T) {
				loader := newContractLoader(t, fakeTracesAPI())
				res := runAdaptiveCmd(t, loader, false, "recommendations", tc.verb, "r1", "--dry-run")
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				assert.Empty(t, res.stdout)
				assert.Contains(t, res.stderr, "[dry-run]")
			})

			t.Run("explicit -o json wins", func(t *testing.T) {
				loader := newContractLoader(t, fakeTracesAPI())
				res := runAdaptiveCmd(t, loader, false, append(append([]string{}, args...), "-o", "json")...)
				require.NoError(t, res.err, "stderr: %s", res.stderr)
				doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
				require.True(t, ok, "stdout document must be an object")
				assert.Equal(t, "gcx.mutation", doc["type"])
			})

			t.Run("agent mode without --force is rejected before mutation", func(t *testing.T) {
				loader := newContractLoader(t, fakeTracesAPI())
				res := runAdaptiveCmd(t, loader, true, "recommendations", tc.verb, "r1")
				require.ErrorIs(t, res.err, providers.ErrAgentModeRequiresForce)
				assert.Empty(t, res.stdout)
			})
		})
	}
}

func TestTracesPoliciesDelete_OutputContract(t *testing.T) {
	t.Run("agent success emits one batch doc", func(t *testing.T) {
		loader := newContractLoader(t, fakeTracesAPI())
		res := runAdaptiveCmd(t, loader, true, "policies", "delete", "p1", "p2", "--force")
		require.NoError(t, res.err, "stderr: %s", res.stderr)

		doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
		require.True(t, ok, "stdout document must be an object")
		assert.Equal(t, "gcx.mutation_batch", doc["type"])
		assert.Equal(t, "1", doc["schema_version"])
		assert.Equal(t, "deleted", doc["action"])
		summary, ok := doc["summary"].(map[string]any)
		require.True(t, ok, "summary must be an object")
		assert.InDelta(t, 2, summary["succeeded"], 0)
		failures, ok := doc["failures"].([]any)
		require.True(t, ok, "failures must always be present ([] on success)")
		assert.Empty(t, failures)
	})

	t.Run("agent partial failure emits one doc and exit 4", func(t *testing.T) {
		loader := newContractLoader(t, fakeTracesAPI("pbad"))
		res := runAdaptiveCmd(t, loader, true, "policies", "delete", "p1", "pbad", "--force")

		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, res.err, &emitted,
			"error = %T (%v), want *gcxerrors.EmittedError", res.err, res.err)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

		doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
		require.True(t, ok, "stdout document must be an object")
		summary, ok := doc["summary"].(map[string]any)
		require.True(t, ok, "summary must be an object")
		assert.InDelta(t, 1, summary["succeeded"], 0)
		assert.InDelta(t, 1, summary["failed"], 0)
		failures, ok := doc["failures"].([]any)
		require.True(t, ok, "failures must be present")
		require.Len(t, failures, 1)
		failure, ok := failures[0].(map[string]any)
		require.True(t, ok, "failure entry must be an object")
		target, ok := failure["target"].(map[string]any)
		require.True(t, ok, "failure target must be an object")
		assert.Equal(t, "policy", target["kind"])
		assert.Equal(t, "pbad", target["id"])
	})

	t.Run("agent total failure keeps the classified error path", func(t *testing.T) {
		loader := newContractLoader(t, fakeTracesAPI("p1", "p2"))
		res := runAdaptiveCmd(t, loader, true, "policies", "delete", "p1", "p2", "--force")

		require.Error(t, res.err)
		var emitted *gcxerrors.EmittedError
		assert.NotErrorAs(t, res.err, &emitted,
			"total failure must use the standard error path, not EmittedError")
		assert.Empty(t, res.stdout,
			"no success-shaped batch document when nothing was deleted — the reporter owns the error document")
		assert.Contains(t, res.err.Error(), "p1")
		assert.Contains(t, res.err.Error(), "p2")
	})

	t.Run("human total failure keeps the classified error path", func(t *testing.T) {
		loader := newContractLoader(t, fakeTracesAPI("p1", "p2"))
		res := runAdaptiveCmd(t, loader, false, "policies", "delete", "p1", "p2", "--force")

		require.Error(t, res.err)
		var emitted *gcxerrors.EmittedError
		assert.NotErrorAs(t, res.err, &emitted)
		assert.Empty(t, res.stdout)
		assert.Contains(t, res.stderr, `Failed to delete policy "p1"`)
		assert.Contains(t, res.stderr, `Failed to delete policy "p2"`)
	})

	t.Run("human default stdout stays empty with per-id prose on stderr", func(t *testing.T) {
		loader := newContractLoader(t, fakeTracesAPI("pbad"))
		res := runAdaptiveCmd(t, loader, false, "policies", "delete", "p1", "pbad", "--force")

		var emitted *gcxerrors.EmittedError
		require.ErrorAs(t, res.err, &emitted,
			"error = %T (%v), want *gcxerrors.EmittedError", res.err, res.err)
		assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

		assert.Empty(t, res.stdout, "default human stdout must stay byte-identical (empty)")
		assert.Contains(t, res.stderr, `Deleted policy "p1"`)
		assert.Contains(t, res.stderr, `Failed to delete policy "pbad"`)
	})

	t.Run("explicit -o json wins", func(t *testing.T) {
		loader := newContractLoader(t, fakeTracesAPI())
		res := runAdaptiveCmd(t, loader, false, "policies", "delete", "p1", "--force", "-o", "json")
		require.NoError(t, res.err, "stderr: %s", res.stderr)
		doc, ok := decodeOneJSONValue(t, res.stdout).(map[string]any)
		require.True(t, ok, "stdout document must be an object")
		assert.Equal(t, "gcx.mutation_batch", doc["type"])
	})

	t.Run("agent mode without --force is rejected before mutation", func(t *testing.T) {
		loader := newContractLoader(t, fakeTracesAPI())
		res := runAdaptiveCmd(t, loader, true, "policies", "delete", "p1")
		require.ErrorIs(t, res.err, providers.ErrAgentModeRequiresForce)
		assert.Empty(t, res.stdout)
	})
}
