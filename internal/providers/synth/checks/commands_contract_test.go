package checks_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/synth/checks"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// These tests pin the agent output contract for the checks mutation commands
// (create, update, delete) and the get/status/timeline diagnostics paths:
//   - agent mode emits exactly one JSON value on stdout;
//   - the human default output stays byte-identical to the pre-codec lines;
//   - partial failures return *gcxerrors.EmittedError with ExitPartialFailure;
//   - explicit -o json/yaml overrides are honored;
//   - advisory warnings and progress notes land on stderr, never stdout.

// contractStatusLoader implements smcfg.StatusLoader for command-level tests:
// SM API calls go direct to the fake server (empty proxy UID) and Grafana
// REST calls (Prometheus queries) hit the same server. When
// promDatasourceUID is set, LoadConfig resolves it as the default Prometheus
// datasource so status queries succeed against the fake query endpoint;
// when empty, datasource resolution fails and status fetches error out.
type contractStatusLoader struct {
	baseURL           string
	namespace         string
	promDatasourceUID string
}

func (l *contractStatusLoader) LoadSMConfig(_ context.Context) (string, string, string, error) {
	return l.baseURL, "test-token", l.namespace, nil
}

func (l *contractStatusLoader) LoadSMProxyConfig(_ context.Context) (config.NamespacedRESTConfig, string, string, error) {
	return config.NamespacedRESTConfig{}, "", l.namespace, nil
}

func (l *contractStatusLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: l.baseURL}, Namespace: l.namespace}, nil
}

func (l *contractStatusLoader) LoadConfig(_ context.Context) (*config.Config, error) {
	if l.promDatasourceUID == "" {
		return &config.Config{}, nil
	}
	return &config.Config{
		CurrentContext: "test",
		Contexts: map[string]*config.Context{
			"test": {Datasources: map[string]string{"prometheus": l.promDatasourceUID}},
		},
	}, nil
}

func (l *contractStatusLoader) SaveMetricsDatasourceUID(_ context.Context, _ string) error {
	return nil
}

var _ smcfg.StatusLoader = &contractStatusLoader{}

// checkAPIState drives the fake SM API.
type checkAPIState struct {
	mu           sync.Mutex
	checks       map[int64]checks.Check
	probesOnline bool
	failCreate   bool
	failDelete   map[int64]bool
}

func newCheckServer(t *testing.T, st *checkAPIState) *httptest.Server {
	t.Helper()
	if st.checks == nil {
		st.checks = map[int64]checks.Check{}
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/v1/probe/list", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, []map[string]any{
			{"id": 1, "name": "Oregon", "online": st.probesOnline},
		})
	})
	mux.HandleFunc("/api/v1/tenant", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, checks.Tenant{ID: 214})
	})
	mux.HandleFunc("/api/v1/check/list", func(w http.ResponseWriter, _ *http.Request) {
		st.mu.Lock()
		list := make([]checks.Check, 0, len(st.checks))
		for _, c := range st.checks {
			list = append(list, c)
		}
		st.mu.Unlock()
		writeJSON(w, list)
	})
	mux.HandleFunc("/api/v1/check/add", func(w http.ResponseWriter, r *http.Request) {
		if st.failCreate {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": "boom"})
			return
		}
		var c checks.Check
		_ = json.NewDecoder(r.Body).Decode(&c)
		c.ID = 1234
		st.mu.Lock()
		st.checks[c.ID] = c
		st.mu.Unlock()
		writeJSON(w, c)
	})
	mux.HandleFunc("/api/v1/check/update", func(w http.ResponseWriter, r *http.Request) {
		var c checks.Check
		_ = json.NewDecoder(r.Body).Decode(&c)
		writeJSON(w, c)
	})
	mux.HandleFunc("/api/v1/check/delete/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/check/delete/")
		var id int64
		_, _ = fmt.Sscanf(idStr, "%d", &id)
		if st.failDelete[id] {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": "boom"})
			return
		}
		writeJSON(w, map[string]string{"msg": "deleted"})
	})
	mux.HandleFunc("/api/v1/check/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/check/")
		var id int64
		_, _ = fmt.Sscanf(idStr, "%d", &id)
		st.mu.Lock()
		c, ok := st.checks[id]
		st.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		writeJSON(w, c)
	})

	// Grafana unified datasource query API (Prometheus) — always empty
	// results so timeline exercises the no-data path deterministically.
	emptyQuery := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"results": map[string]any{}})
	}
	mux.HandleFunc("/apis/query.grafana.app/v0alpha1/namespaces/default/query", emptyQuery)
	mux.HandleFunc("/api/ds/query", emptyQuery)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// runChecks executes a `checks` subcommand against the fake server, capturing
// stdout and stderr. The command tree is built after the agent flag is set,
// mirroring the real CLI (BindFlags reads agent mode at construction time).
func runChecks(t *testing.T, srvURL string, agentMode bool, stdin string, args ...string) (string, string, error) {
	t.Helper()
	return runChecksLoader(t, &contractStatusLoader{baseURL: srvURL, namespace: "default"}, agentMode, stdin, args...)
}

// runChecksLoader is runChecks with an explicit loader, for tests that need
// non-default loader behavior (e.g. a resolvable Prometheus datasource).
func runChecksLoader(t *testing.T, loader smcfg.StatusLoader, agentMode bool, stdin string, args ...string) (string, string, error) {
	t.Helper()
	prevNoColor := color.NoColor
	color.NoColor = true
	agent.SetFlag(agentMode)
	t.Cleanup(func() {
		agent.SetFlag(false)
		color.NoColor = prevNoColor
	})

	root := checks.Commands(loader)
	root.SilenceErrors = true
	root.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

// decodeSingleJSONValue asserts stdout carries exactly one JSON value
// followed by EOF, and returns it.
func decodeSingleJSONValue(t *testing.T, stdout string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc any
	require.NoError(t, dec.Decode(&doc), "stdout must be valid JSON, got: %q", stdout)
	require.ErrorIs(t, dec.Decode(new(any)), io.EOF, "stdout must contain exactly one JSON value, got: %q", stdout)
	return doc
}

// jsonInt converts a JSON-decoded numeric field to int for exact assertions.
func jsonInt(t *testing.T, v any) int {
	t.Helper()
	f, ok := v.(float64)
	require.True(t, ok, "expected JSON number, got %T", v)
	return int(f)
}

func writeCheckManifest(t *testing.T, dir, file string) string {
	t.Helper()
	content := `apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1
kind: Check
metadata:
  name: web-check
spec:
  job: web-check
  target: https://example.com
  frequency: 60000
  timeout: 10000
  enabled: true
  probes:
    - Oregon
  settings:
    http:
      method: GET
`
	path := filepath.Join(dir, file)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestChecksCreateOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		extraArgs  []string
		wantStdout string
		checkJSON  bool
		wantInOut  string
	}{
		{
			name:       "human default byte-identical",
			wantStdout: "✔ Created check \"web-check\" (id=1234)\n",
		},
		{
			name:      "agent mode single JSON document",
			agentMode: true,
			checkJSON: true,
		},
		{
			name:      "explicit -o json override",
			extraArgs: []string{"-o", "json"},
			checkJSON: true,
		},
		{
			name:      "explicit -o yaml override",
			extraArgs: []string{"-o", "yaml"},
			wantInOut: "type: gcx.synth.check_create",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &checkAPIState{probesOnline: true}
			srv := newCheckServer(t, st)
			manifest := writeCheckManifest(t, t.TempDir(), "check.yaml")

			args := append([]string{"create", "-f", manifest}, tc.extraArgs...)
			stdout, stderr, err := runChecks(t, srv.URL, tc.agentMode, "", args...)
			require.NoError(t, err)
			assert.NotContains(t, stderr, "offline")

			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
			}
			if tc.wantInOut != "" {
				assert.Contains(t, stdout, tc.wantInOut)
			}
			if tc.checkJSON {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok, "create result must be a JSON object")
				assert.Equal(t, "gcx.synth.check_create", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, "web-check", doc["job"])
				assert.Equal(t, 1234, jsonInt(t, doc["id"]))
				assert.Equal(t, "web-check-1234", doc["name"])
			}
		})
	}
}

func TestChecksCreateOfflineProbesWarningOnStderr(t *testing.T) {
	st := &checkAPIState{probesOnline: false}
	srv := newCheckServer(t, st)
	manifest := writeCheckManifest(t, t.TempDir(), "check.yaml")

	stdout, stderr, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest)
	require.NoError(t, err)

	// The pre-create warning is a diagnostic: stderr only, stdout keeps the
	// byte-identical result line.
	assert.Contains(t, stderr, "all probes for check \"web-check\" are offline")
	assert.Equal(t, "✔ Created check \"web-check\" (id=1234)\n", stdout)
}

func TestChecksUpdateOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		extraArgs  []string
		wantStdout string
		checkJSON  bool
	}{
		{
			name:       "human default byte-identical",
			wantStdout: "✔ Updated check \"web-check\" (id=1234)\n",
		},
		{
			name:      "agent mode single JSON document",
			agentMode: true,
			checkJSON: true,
		},
		{
			name:      "explicit -o json override",
			extraArgs: []string{"-o", "json"},
			checkJSON: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &checkAPIState{probesOnline: true}
			srv := newCheckServer(t, st)
			manifest := writeCheckManifest(t, t.TempDir(), "check.yaml")

			args := append([]string{"update", "web-check-1234", "-f", manifest}, tc.extraArgs...)
			stdout, _, err := runChecks(t, srv.URL, tc.agentMode, "", args...)
			require.NoError(t, err)

			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
			}
			if tc.checkJSON {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.synth.check_update", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, 1234, jsonInt(t, doc["id"]))
				assert.Equal(t, "web-check-1234", doc["name"])
			}
		})
	}
}

func TestChecksDeleteOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		extraArgs  []string
		wantStdout string
		checkJSON  bool
	}{
		{
			name:       "human default byte-identical",
			wantStdout: "✔ Deleted check web-check-1234\n✔ Deleted check web-check-5678\n",
		},
		{
			name:      "agent mode single JSON document",
			agentMode: true,
			checkJSON: true,
		},
		{
			name:      "explicit -o json override",
			extraArgs: []string{"-o", "json"},
			checkJSON: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &checkAPIState{probesOnline: true}
			srv := newCheckServer(t, st)

			args := append([]string{"delete", "web-check-1234", "web-check-5678", "--force"}, tc.extraArgs...)
			stdout, _, err := runChecks(t, srv.URL, tc.agentMode, "", args...)
			require.NoError(t, err)

			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
			}
			if tc.checkJSON {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.synth.delete_batch", doc["type"])
				assert.Equal(t, []any{"web-check-1234", "web-check-5678"}, doc["deleted"])
			}
		})
	}
}

func TestChecksDeletePartialFailure(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode"},
		{name: "agent mode", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &checkAPIState{probesOnline: true, failDelete: map[int64]bool{5678: true}}
			srv := newCheckServer(t, st)

			stdout, stderr, err := runChecks(t, srv.URL, tc.agentMode, "", "delete", "web-check-1234", "web-check-5678", "--force")

			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			assert.Contains(t, stderr, "deleting check web-check-5678")

			if tc.agentMode {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.synth.delete_batch", doc["type"])
				assert.Equal(t, []any{"web-check-1234"}, doc["deleted"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 1, jsonInt(t, summary["succeeded"]))
				assert.Equal(t, 1, jsonInt(t, summary["failed"]))
			} else {
				assert.Equal(t, "✔ Deleted check web-check-1234\n", stdout)
			}
		})
	}
}

func TestChecksDeletePromptOnStderr(t *testing.T) {
	st := &checkAPIState{probesOnline: true}
	srv := newCheckServer(t, st)

	stdout, stderr, err := runChecks(t, srv.URL, false, "n\n", "delete", "web-check-1234")
	require.NoError(t, err)
	assert.Empty(t, stdout, "prompt and decline note must not touch stdout")
	assert.Contains(t, stderr, "Delete 1 check(s)? [y/N]")
	assert.Contains(t, stderr, "Aborted.")
}

func TestChecksGetDiagnosticsOnStderr(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1234: {ID: 1234, Job: "web-check", Target: "https://example.com",
				Settings: checks.CheckSettings{"http": map[string]any{"method": "GET"}}},
		},
	}
	srv := newCheckServer(t, st)

	t.Run("structured format carries fetched status in-band", func(t *testing.T) {
		// Pattern 13: --show-status fetches regardless of output format. The
		// fake query endpoint returns no series, so the computed status is
		// NODATA — merged into the document as the top-level status member.
		loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", promDatasourceUID: "test-uid"}
		stdout, stderr, err := runChecksLoader(t, loader, false, "", "get", "web-check-1234", "-o", "json", "--show-status")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Check", doc["kind"])
		status, ok := doc["status"].(map[string]any)
		require.True(t, ok, "--show-status must merge the fetched status into the structured output: %s", stdout)
		assert.Equal(t, "NODATA", status["status"])
		assert.NotContains(t, status, "success", "success is omitted when the query returned no data")
		assert.NotContains(t, stderr, "--show-status")
	})

	t.Run("structured format without --show-status has no status member", func(t *testing.T) {
		loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", promDatasourceUID: "test-uid"}
		stdout, _, err := runChecksLoader(t, loader, false, "", "get", "web-check-1234", "-o", "json")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok)
		assert.NotContains(t, doc, "status")
	})

	t.Run("structured format status-query failure warning stays on stderr", func(t *testing.T) {
		// The zero-value config carries no datasource, so the status fetch
		// fails: the warning is a diagnostic on stderr and the document
		// simply omits the status member — never a "flag ignored" skip.
		stdout, stderr, err := runChecks(t, srv.URL, false, "", "get", "web-check-1234", "-o", "json", "--show-status")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Check", doc["kind"])
		assert.NotContains(t, doc, "status")
		assert.Contains(t, stderr, "could not retrieve execution status")
		assert.NotContains(t, stderr, "only applies to table/wide output")
	})

	t.Run("table status-query failure warning stays on stderr", func(t *testing.T) {
		// The zero-value config carries no datasource, so the status query
		// fails; the warning must land on stderr, not contaminate the table.
		stdout, stderr, err := runChecks(t, srv.URL, false, "", "get", "web-check-1234", "--show-status")
		require.NoError(t, err)

		assert.Contains(t, stderr, "could not retrieve execution status")
		assert.NotContains(t, stdout, "could not retrieve execution status")
		assert.Contains(t, stdout, "web-check-1234")
	})
}

func TestChecksStatusEmptyContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		wantStdout string
		checkEmpty bool
	}{
		{name: "human default keeps prose notice", wantStdout: "🛈 No checks found.\n"},
		{name: "agent mode emits one empty JSON document", agentMode: true, checkEmpty: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &checkAPIState{probesOnline: true}
			srv := newCheckServer(t, st)

			stdout, _, err := runChecks(t, srv.URL, tc.agentMode, "", "status", "--datasource-uid", "test-uid")
			require.NoError(t, err)

			if tc.checkEmpty {
				doc, ok := decodeSingleJSONValue(t, stdout).([]any)
				require.True(t, ok, "empty status must encode as a JSON array, got: %q", stdout)
				assert.Empty(t, doc)
			} else {
				assert.Equal(t, tc.wantStdout, stdout)
			}
		})
	}
}

func TestChecksTimelineContract(t *testing.T) {
	newState := func() *checkAPIState {
		return &checkAPIState{
			probesOnline: true,
			checks: map[int64]checks.Check{
				42: {ID: 42, Job: "web", Target: "https://example.com",
					Settings: checks.CheckSettings{"http": map[string]any{}},
					Created:  float64(time.Now().Add(-30 * time.Minute).Unix())},
			},
		}
	}

	t.Run("clamp notice goes to stderr, no-data notice keeps human stdout", func(t *testing.T) {
		srv := newCheckServer(t, newState())

		stdout, stderr, err := runChecks(t, srv.URL, false, "", "timeline", "42", "--datasource-uid", "test-uid")
		require.NoError(t, err)

		// --since default (6h) exceeds check age (30m) → clamped.
		assert.Contains(t, stderr, "window adjusted to match")
		assert.Equal(t, "🛈 No time-series data available for check 42.\n", stdout)
	})

	t.Run("agent mode emits one JSON document even with no data", func(t *testing.T) {
		srv := newCheckServer(t, newState())

		stdout, stderr, err := runChecks(t, srv.URL, true, "", "timeline", "42", "--datasource-uid", "test-uid")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok, "timeline payload must be a JSON object, got: %q", stdout)
		assert.Contains(t, doc, "Series")
		series, ok := doc["Series"].([]any)
		require.True(t, ok, "Series must serialize as an array, not null")
		assert.Empty(t, series)
		assert.Contains(t, stderr, "window adjusted to match")
	})
}
