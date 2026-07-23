package datasources_test

// These tests pin the agent output contract for the datasources commands
// whose documented exit-4 semantics cover per-target failures (delete,
// health): in agent mode the per-target results document is EXACTLY ONE JSON
// value on stdout and the exit signal travels as *gcxerrors.EmittedError
// carrying ExitPartialFailure — never a second error document on stdout.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/datasources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// executeAgentModeCommand runs a datasources command with agent-mode
// detection pinned ON (the output default resolves to the agents codec at
// flag-binding time, so the pin must precede command construction).
func executeAgentModeCommand(t *testing.T, args []string) (string, error) {
	t.Helper()

	testutils.SetAgentMode(t, true)

	root := helperRoot(datasources.Command())
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
	return stdout.String(), err
}

// decodeExactlyOneJSONValue asserts stdout carries exactly one JSON value
// followed by EOF, and returns the decoded value.
func decodeExactlyOneJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var first any
	require.NoError(t, dec.Decode(&first), "stdout is not valid JSON: %q", stdout)
	var second any
	require.ErrorIs(t, dec.Decode(&second), io.EOF,
		"stdout must contain exactly one JSON value: %q", stdout)
	return first
}

func TestDeleteAgentModePartialFailure_OutputContract(t *testing.T) {
	store := map[string]map[string]any{
		"exists": {"uid": "exists", "name": "exists", "type": "grafana-mock-datasource"},
	}
	calls := &crudCalls{}
	server := newCRUDServer(t, store, calls)
	defer server.Close()
	cfg := newConfigFileForServer(t, server.URL)

	stdout, err := executeAgentModeCommand(t,
		[]string{"datasources", "delete", "--config", cfg, "exists", "missing", "--yes"})

	// The exit signal is EmittedError(4): the results document already
	// enumerates the per-UID outcomes, so the reporter must not write a
	// second error document to stdout.
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted,
		"error = %T (%v), want *gcxerrors.EmittedError", err, err)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

	// The classified cause stays reachable for the reporter's metadata.
	var pf *gcxerrors.PartialFailureError
	require.ErrorAs(t, err, &pf)

	doc := decodeExactlyOneJSONValue(t, stdout)
	assert.Contains(t, stdout, `"deleted"`)
	assert.Contains(t, stdout, `"failed"`)
	assert.NotNil(t, doc)
	assert.Equal(t, 2, calls.del, "delete is attempted per UID")
}

// newHealthServer serves the legacy /api/datasources REST API with a
// per-UID health verdict: uids in unhealthy report status ERROR.
func newHealthServer(t *testing.T, store map[string]map[string]any, unhealthy map[string]bool) *httptest.Server {
	t.Helper()

	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/apis" || path == "/bootdata":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && strings.HasSuffix(path, "/health"):
			uid := strings.TrimSuffix(strings.TrimPrefix(path, "/api/datasources/uid/"), "/health")
			if unhealthy[uid] {
				writeJSON(w, map[string]any{"status": "ERROR", "message": "connection refused"})
				return
			}
			writeJSON(w, map[string]any{"status": "OK", "message": "OK"})
		case r.Method == http.MethodGet && path == "/api/datasources":
			items := make([]map[string]any, 0, len(store))
			for _, ds := range store {
				items = append(items, ds)
			}
			writeJSON(w, items)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/datasources/uid/"):
			uid := strings.TrimPrefix(path, "/api/datasources/uid/")
			ds, ok := store[uid]
			if !ok {
				http.Error(w, `{"message":"Data source not found"}`, http.StatusNotFound)
				return
			}
			writeJSON(w, ds)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, path)
			http.Error(w, "unexpected request", http.StatusInternalServerError)
		}
	}))
}

func TestHealthAgentModeUnhealthy_OutputContract(t *testing.T) {
	store := map[string]map[string]any{
		"good": {"uid": "good", "name": "good", "type": "prometheus"},
		"bad":  {"uid": "bad", "name": "bad", "type": "prometheus"},
	}
	server := newHealthServer(t, store, map[string]bool{"bad": true})
	defer server.Close()
	cfg := newConfigFileForServer(t, server.URL)

	stdout, err := executeAgentModeCommand(t,
		[]string{"datasources", "health", "--config", cfg})

	// Documented contract: exit 4 means the check ran and found unhealthy
	// datasources. The verdict document is already on stdout, so the exit
	// signal must travel as EmittedError — never a second error document.
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted,
		"error = %T (%v), want *gcxerrors.EmittedError", err, err)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)

	doc := decodeExactlyOneJSONValue(t, stdout)
	assert.NotNil(t, doc)
	assert.Contains(t, stdout, `"ERROR"`)
	assert.Contains(t, stdout, "connection refused")
	assert.Contains(t, stdout, `"OK"`)
}
