package checks_test

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"github.com/grafana/gcx/internal/query/dataframe"
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
	// smDatasourceUID, when set, switches the loader to proxy mode: SM API calls
	// go through /api/datasources/proxy/uid/<uid>/sm/ and named queries can run
	// against the SM datasource. Empty keeps SM API calls direct.
	smDatasourceUID string
}

func (l *contractStatusLoader) LoadSMConfig(_ context.Context) (string, string, string, error) {
	return l.baseURL, "test-token", l.namespace, nil
}

func (l *contractStatusLoader) LoadSMProxyConfig(_ context.Context) (config.NamespacedRESTConfig, string, string, error) {
	if l.smDatasourceUID == "" {
		return config.NamespacedRESTConfig{}, "", l.namespace, nil
	}
	return config.NamespacedRESTConfig{Config: rest.Config{Host: l.baseURL}, Namespace: l.namespace}, l.smDatasourceUID, l.namespace, nil
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

func (l *contractStatusLoader) SaveLogsDatasourceUID(_ context.Context, _ string) error {
	return nil
}

var _ smcfg.AdHocLoader = &contractStatusLoader{}

// checkAPIState drives the fake SM API.
type checkAPIState struct {
	mu           sync.Mutex
	checks       map[int64]checks.Check
	probesOnline bool
	failCreate   bool
	failGet      bool // GET check/<id> answers 500
	failDelete   map[int64]bool
	lastUpdated  checks.Check // last body posted to /api/v1/check/update
	// writes counts POSTs to check/add and check/update, so dry-run tests can
	// prove nothing was persisted.
	writes int
	// validateStatus, when non-zero, enables /api/v1/check/validate and makes it
	// answer with this status and validateBody. When zero the endpoint is absent
	// (404), as on a server that predates it.
	validateStatus int
	validateBody   any
	validateCalls  int
	lastValidate   map[string]any // last body posted to /api/v1/check/validate
	// adhocLines, when non-empty, are served as raw Loki log lines from the
	// query endpoints (as `checks test` polls) instead of an empty result.
	adhocLines []string
	// namedFrames serves SM named queries (checks_reachability, ...) by queryType.
	// namedLatency serves checks_latency per checkType. Both are empty by default.
	namedFrames  map[string][]dataframe.Frame
	namedLatency map[string][]dataframe.Frame
	// namedCalls records "queryType" (or "checks_latency:<checkType>") per request.
	namedCalls []string
	// namedErrors answers the named query with that name with a per-refId error
	// (HTTP 200), as the plugin does for an unknown or rejected query. The key
	// "*" applies to every named query.
	namedErrors map[string]string
}

// writeCount, validateCount and lastValidateBody read the fixture counters under
// st.mu; handlers write them from the server goroutine.
func (st *checkAPIState) writeCount() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.writes
}

func (st *checkAPIState) validateCount() int {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.validateCalls
}

func (st *checkAPIState) lastValidateBody() map[string]any {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.lastValidate
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
		st.writes++
		st.checks[c.ID] = c
		st.mu.Unlock()
		writeJSON(w, c)
	})
	mux.HandleFunc("/api/v1/check/update", func(w http.ResponseWriter, r *http.Request) {
		var c checks.Check
		_ = json.NewDecoder(r.Body).Decode(&c)
		st.mu.Lock()
		st.writes++
		st.lastUpdated = c
		st.mu.Unlock()
		writeJSON(w, c)
	})
	if st.validateStatus != 0 {
		mux.HandleFunc("/api/v1/check/validate", func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			st.mu.Lock()
			st.validateCalls++
			st.lastValidate = body
			st.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(st.validateStatus)
			_ = json.NewEncoder(w).Encode(st.validateBody)
		})
	}
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
	mux.HandleFunc("/api/v1/check/adhoc", func(w http.ResponseWriter, r *http.Request) {
		var req checks.AdHocCheckRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeJSON(w, checks.AdHocCheckResponse{
			ID:       "abc-123",
			TenantID: 214,
			Timeout:  req.Timeout,
			Settings: req.Settings,
			Probes:   req.Probes,
			Target:   req.Target,
		})
	})
	mux.HandleFunc("/api/v1/check/", func(w http.ResponseWriter, r *http.Request) {
		if st.failGet {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": "boom"})
			return
		}
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

	// Grafana unified datasource query API (Prometheus/Loki) — empty results
	// by default so timeline/test exercise the no-data path deterministically,
	// unless adhocLines is set (checks test's Loki polling contract test).
	query := func(w http.ResponseWriter, r *http.Request) {
		if serveNamedQuery(st, w, r) {
			return
		}
		st.mu.Lock()
		lines := st.adhocLines
		st.mu.Unlock()
		if len(lines) == 0 {
			writeJSON(w, map[string]any{"results": map[string]any{}})
			return
		}
		writeJSON(w, adhocQueryResponse(lines))
	}
	mux.HandleFunc("/apis/query.grafana.app/v0alpha1/namespaces/default/query", query)
	mux.HandleFunc("/api/ds/query", query)

	// Proxy mode: /api/datasources/proxy/uid/<uid>/sm/<p> is the SM API's /api/v1/<p>.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rest, ok := strings.CutPrefix(r.URL.Path, "/api/datasources/proxy/uid/"); ok {
			if _, after, found := strings.Cut(rest, "/sm/"); found {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/api/v1/" + after
				mux.ServeHTTP(w, r2)
				return
			}
		}
		mux.ServeHTTP(w, r)
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// smSeriesFrame is one instant-vector series labelled with (job, instance), the
// shape the SM backend returns for tenant-wide queries.
func smSeriesFrame(job, instance string, value float64) dataframe.Frame {
	return dataframe.Frame{
		Schema: dataframe.Schema{Fields: []dataframe.Field{
			{Name: "Time", Type: "time"},
			{Name: "Value", Type: "number", Labels: map[string]string{"job": job, "instance": instance}},
		}},
		Data: dataframe.Data{Values: [][]any{{float64(1000)}, {value}}},
	}
}

// serveNamedQuery answers an SM named-query request from st, reporting whether
// the request was one. Anything else falls through to the generic query handler.
func serveNamedQuery(st *checkAPIState, w http.ResponseWriter, r *http.Request) bool {
	var body struct {
		Queries []map[string]any `json:"queries"`
	}
	raw, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(raw, &body); err != nil || len(body.Queries) != 1 {
		r.Body = io.NopCloser(bytes.NewReader(raw))
		return false
	}
	qt, _ := body.Queries[0]["queryType"].(string)
	if !strings.HasPrefix(qt, "checks_") {
		r.Body = io.NopCloser(bytes.NewReader(raw))
		return false
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	frames := st.namedFrames[qt]
	call := qt
	if qt == "checks_latency" {
		ct, _ := body.Queries[0]["checkType"].(string)
		frames = st.namedLatency[ct]
		call = qt + ":" + ct
	}
	st.namedCalls = append(st.namedCalls, call)

	msg, failing := st.namedErrors[qt]
	if !failing {
		msg, failing = st.namedErrors["*"]
	}
	if failing {
		writeJSON(w, dataframe.Response{Results: map[string]dataframe.Result{
			"A": {Status: 400, Error: msg},
		}})
		return true
	}

	writeJSON(w, dataframe.Response{Results: map[string]dataframe.Result{
		"A": {Status: 200, Frames: frames},
	}})
	return true
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
func runChecksLoader(t *testing.T, loader smcfg.AdHocLoader, agentMode bool, stdin string, args ...string) (string, string, error) {
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

// adhocQueryResponse builds a Grafana datasource-query response carrying the
// given raw Loki log line bodies, in the shape checks.PollAdHocResults expects.
func adhocQueryResponse(lines []string) dataframe.Response {
	values := make([]any, len(lines))
	labels := make([]any, len(lines))
	times := make([]any, len(lines))
	for i, l := range lines {
		values[i] = l
		labels[i] = map[string]any{"type": "adhoc"}
		times[i] = float64(1711893600000)
	}
	return dataframe.Response{
		Results: map[string]dataframe.Result{
			"A": {
				Frames: []dataframe.Frame{
					{
						Schema: dataframe.Schema{
							Fields: []dataframe.Field{
								{Name: "labels", Type: "other"},
								{Name: "Time", Type: "time"},
								{Name: "Line", Type: "string"},
							},
						},
						Data: dataframe.Data{Values: [][]any{labels, times, values}},
					},
				},
			},
		},
	}
}

func writeCheckManifest(t *testing.T, dir string) string {
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
	path := filepath.Join(dir, "check.yaml")
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
			manifest := writeCheckManifest(t, t.TempDir())

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
	manifest := writeCheckManifest(t, t.TempDir())

	stdout, stderr, err := runChecks(t, srv.URL, false, "", "create", "-f", manifest)
	require.NoError(t, err)

	// The pre-create warning is a diagnostic: stderr only, stdout keeps the
	// byte-identical result line.
	assert.Contains(t, stderr, "all probes for check \"web-check\" are offline")
	assert.Equal(t, "✔ Created check \"web-check\" (id=1234)\n", stdout)
}

func TestChecksTestOutputContract(t *testing.T) {
	adhocLine := func(probeName string, success float64) string {
		data, err := json.Marshal(map[string]any{
			"id":    "abc-123",
			"probe": probeName,
			"logs":  []any{map[string]any{"level": "info", "msg": "starting probe"}},
			"timeseries": []any{
				map[string]any{
					"name":   "probe_success",
					"metric": []any{map[string]any{"gauge": map[string]any{"value": success}}},
				},
			},
		})
		require.NoError(t, err)
		return string(data)
	}

	st := &checkAPIState{probesOnline: true, adhocLines: []string{adhocLine("Oregon", 1)}}
	srv := newCheckServer(t, st)
	manifest := writeCheckManifest(t, t.TempDir())

	stdout, _, err := runChecks(t, srv.URL, false, "", "test", "-f", manifest, "--logs-datasource-uid", "loki-uid")
	require.NoError(t, err)
	assert.Contains(t, stdout, "Oregon")
	assert.Contains(t, stdout, "success")
	assert.Contains(t, stdout, "Ran ad-hoc check \"web-check\" against 1 probe(s): 1 succeeded, 0 failed, 0 timed out")

	stdout, _, err = runChecks(t, srv.URL, true, "", "test", "-f", manifest, "--logs-datasource-uid", "loki-uid")
	require.NoError(t, err)
	doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
	require.True(t, ok, "test result must be a JSON object")
	assert.Equal(t, "gcx.synth.check_test", doc["type"])
	assert.Equal(t, "abc-123", doc["adhoc_id"])
	probesOut, ok := doc["probes"].([]any)
	require.True(t, ok)
	require.Len(t, probesOut, 1)
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
			manifest := writeCheckManifest(t, t.TempDir())

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
		loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", smDatasourceUID: "sm-uid"}
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

	t.Run("status reads this check's reachability row", func(t *testing.T) {
		withData := &checkAPIState{
			probesOnline: true,
			checks: map[int64]checks.Check{
				1234: {ID: 1234, Job: "web-check", Target: "https://example.com",
					Settings: checks.CheckSettings{"http": map[string]any{"method": "GET"}}},
			},
			namedFrames: map[string][]dataframe.Frame{
				"checks_reachability": {
					smSeriesFrame("other", "https://other.example", 0.1),
					smSeriesFrame("web-check", "https://example.com", 0.98),
				},
			},
		}
		dataSrv := newCheckServer(t, withData)
		loader := &contractStatusLoader{baseURL: dataSrv.URL, namespace: "default", smDatasourceUID: "sm-uid"}

		stdout, _, err := runChecksLoader(t, loader, false, "", "get", "web-check-1234", "-o", "json", "--show-status")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok)
		status, ok := doc["status"].(map[string]any)
		require.True(t, ok, "status member missing: %s", stdout)
		assert.Equal(t, "OK", status["status"])
		assert.InDelta(t, 0.98, status["success"], 1e-9)

		// One tenant-wide call, shared with `checks status`, not one per check.
		withData.mu.Lock()
		defer withData.mu.Unlock()
		assert.Equal(t, []string{"checks_reachability"}, withData.namedCalls)
	})

	t.Run("structured format without --show-status has no status member", func(t *testing.T) {
		loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", smDatasourceUID: "sm-uid"}
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

func TestChecksGetDecodeScript(t *testing.T) {
	plaintext := "export default function() { console.log('hi'); }"
	encoded := base64.StdEncoding.EncodeToString([]byte(plaintext))
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1234: {ID: 1234, Job: "web-check", Target: "https://example.com",
				Settings: checks.CheckSettings{"scripted": map[string]any{"script": encoded}}},
		},
	}
	srv := newCheckServer(t, st)

	t.Run("yaml/json output decodes the script", func(t *testing.T) {
		stdout, _, err := runChecks(t, srv.URL, false, "", "get", "web-check-1234", "-o", "json", "--decode-script")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok)
		spec, ok := doc["spec"].(map[string]any)
		require.True(t, ok)
		settings, ok := spec["settings"].(map[string]any)
		require.True(t, ok)
		scripted, ok := settings["scripted"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, plaintext, scripted["script"])
	})

	t.Run("without the flag the script stays base64", func(t *testing.T) {
		stdout, _, err := runChecks(t, srv.URL, false, "", "get", "web-check-1234", "-o", "json")
		require.NoError(t, err)

		doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
		require.True(t, ok)
		spec, ok := doc["spec"].(map[string]any)
		require.True(t, ok)
		settings, ok := spec["settings"].(map[string]any)
		require.True(t, ok)
		scripted, ok := settings["scripted"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, encoded, scripted["script"])
	})

	t.Run("table output warns and ignores the flag", func(t *testing.T) {
		stdout, stderr, err := runChecks(t, srv.URL, false, "", "get", "web-check-1234", "--decode-script")
		require.NoError(t, err)

		assert.Contains(t, stderr, "--decode-script has no effect on table output")
		assert.Contains(t, stdout, "web-check-1234")
		assert.NotContains(t, stdout, plaintext)
	})
}

func TestChecksUpdateEncodesPlaintextScript(t *testing.T) {
	plaintext := "export default function() { console.log('hi'); }"
	st := &checkAPIState{probesOnline: true}
	srv := newCheckServer(t, st)

	dir := t.TempDir()
	manifest := filepath.Join(dir, "check.yaml")
	content := "apiVersion: syntheticmonitoring.ext.grafana.app/v1alpha1\n" +
		"kind: Check\n" +
		"metadata:\n" +
		"  name: web-check\n" +
		"spec:\n" +
		"  job: web-check\n" +
		"  target: https://example.com\n" +
		"  frequency: 60000\n" +
		"  timeout: 10000\n" +
		"  enabled: true\n" +
		"  probes:\n" +
		"    - Oregon\n" +
		"  settings:\n" +
		"    scripted:\n" +
		"      script: |-\n" +
		"        " + plaintext + "\n"
	require.NoError(t, os.WriteFile(manifest, []byte(content), 0o600))

	_, _, err := runChecks(t, srv.URL, false, "", "update", "web-check-1234", "-f", manifest)
	require.NoError(t, err)

	st.mu.Lock()
	sent := st.lastUpdated
	st.mu.Unlock()

	scripted, ok := sent.Settings["scripted"].(map[string]any)
	require.True(t, ok, "settings sent to the API must still be scripted: %+v", sent.Settings)
	sentScript, ok := scripted["script"].(string)
	require.True(t, ok)

	decoded, err := base64.StdEncoding.DecodeString(sentScript)
	require.NoError(t, err, "script sent to the API must be base64-encoded")
	assert.Equal(t, plaintext, string(decoded))
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

			stdout, _, err := runChecks(t, srv.URL, tc.agentMode, "", "status")
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

// TestChecksStatusAcceptsDeprecatedDatasourceUID pins that scripts which still
// pass the old Prometheus-datasource flag keep working: status no longer reads
// it, but removing it outright would turn a no-op into a usage error.
func TestChecksStatusAcceptsDeprecatedDatasourceUID(t *testing.T) {
	srv := newCheckServer(t, &checkAPIState{probesOnline: true})

	_, _, err := runChecks(t, srv.URL, false, "", "status", "--datasource-uid", "test-uid")
	require.NoError(t, err)
}

// TestChecksStatusReadsNamedQueries pins the whole status path against the SM
// datasource: no Prometheus datasource is configured, the three named queries are
// the only source of metrics, and latency is requested once per check type.
func TestChecksStatusReadsNamedQueries(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1: {ID: 1, Job: "web", Target: "https://a", Probes: []int64{1},
				Settings: checks.CheckSettings{"http": map[string]any{}}},
			2: {ID: 2, Job: "script", Target: "https://b", Probes: []int64{1},
				Settings: checks.CheckSettings{"scripted": map[string]any{}}},
		},
		namedFrames: map[string][]dataframe.Frame{
			"checks_reachability": {smSeriesFrame("web", "https://a", 0.99), smSeriesFrame("script", "https://b", 0.5)},
			"checks_probe_count":  {smSeriesFrame("web", "https://a", 1.0), smSeriesFrame("script", "https://b", 1.0)},
		},
		namedLatency: map[string][]dataframe.Frame{
			"http":     {smSeriesFrame("web", "https://a", 0.25), smSeriesFrame("script", "https://b", 99.0)},
			"scripted": {smSeriesFrame("web", "https://a", 77.0), smSeriesFrame("script", "https://b", 2.0)},
		},
	}
	srv := newCheckServer(t, st)
	loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", smDatasourceUID: "sm-uid"}

	stdout, stderr, err := runChecksLoader(t, loader, false, "", "status", "-o", "json")
	require.NoError(t, err)
	assert.NotContains(t, stderr, "unavailable")

	docs, ok := decodeSingleJSONValue(t, stdout).([]any)
	require.True(t, ok)
	require.Len(t, docs, 2)

	byJob := map[string]map[string]any{}
	for _, d := range docs {
		row, ok := d.(map[string]any)
		require.True(t, ok)
		job, _ := row["job"].(string)
		byJob[job] = row
	}

	// Each check reads the latency row from the call made with its own type.
	assert.InDelta(t, 0.99, byJob["web"]["success"], 1e-9)
	assert.InDelta(t, 250.0, byJob["web"]["latencyMs"], 1e-9)
	assert.Equal(t, "OK", byJob["web"]["status"])
	assert.InDelta(t, 2000.0, byJob["script"]["latencyMs"], 1e-9)
	assert.Equal(t, "FAILING", byJob["script"]["status"])

	st.mu.Lock()
	defer st.mu.Unlock()
	assert.ElementsMatch(t,
		[]string{"checks_reachability", "checks_probe_count", "checks_latency:http", "checks_latency:scripted"},
		st.namedCalls)
}

// TestChecksStatusFailsWhenNoQueryIsAnswered pins that a backend which answers
// none of the queries (for example an SM app that predates them) is an error, not
// a table of NODATA that reads as "these checks have no data".
func TestChecksStatusFailsWhenNoQueryIsAnswered(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1: {ID: 1, Job: "web", Target: "https://a", Settings: checks.CheckSettings{"http": map[string]any{}}},
		},
		namedErrors: map[string]string{"*": "unknown query"},
	}
	srv := newCheckServer(t, st)
	loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", smDatasourceUID: "sm-uid"}

	stdout, _, err := runChecksLoader(t, loader, false, "", "status", "-o", "json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checks_reachability")
	assert.Contains(t, err.Error(), "unknown query")
	assert.Empty(t, stdout, "no document should be emitted when nothing could be read")
}

// TestChecksStatusWithoutSMDatasource pins the direct-API case: the SM API is
// reachable but no SM datasource resolves (no UID), so status cannot be read.
// It must fail fast with the way out, not with the client's bare "uid is
// required", and without listing checks it cannot give a status for.
func TestChecksStatusWithoutSMDatasource(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1: {ID: 1, Job: "web", Target: "https://a", Settings: checks.CheckSettings{"http": map[string]any{}}},
		},
	}
	srv := newCheckServer(t, st)

	stdout, _, err := runChecks(t, srv.URL, false, "", "status", "-o", "json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Synthetic Monitoring datasource")
	assert.Contains(t, err.Error(), "datasources.synthetic-monitoring", "the error must say how to pin the datasource")
	assert.Empty(t, stdout)

	st.mu.Lock()
	defer st.mu.Unlock()
	assert.Empty(t, st.namedCalls, "no query should be attempted without a datasource")
}

// TestChecksGetShowStatusWithoutSMDatasource pins that --show-status in the same
// situation still returns the check and puts the same hint on stderr.
func TestChecksGetShowStatusWithoutSMDatasource(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1234: {ID: 1234, Job: "web-check", Target: "https://example.com",
				Settings: checks.CheckSettings{"http": map[string]any{"method": "GET"}}},
		},
	}
	srv := newCheckServer(t, st)

	_, stderr, err := runChecks(t, srv.URL, false, "", "get", "web-check-1234", "-o", "json", "--show-status")
	require.NoError(t, err)
	assert.Contains(t, stderr, "no Synthetic Monitoring datasource")
	assert.Contains(t, stderr, "datasources.synthetic-monitoring")
}

// TestChecksStatusErrorNamesEachFailureOnce pins that latency, queried once per
// check type, does not repeat the same failure in the error for every type.
func TestChecksStatusErrorNamesEachFailureOnce(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1: {ID: 1, Job: "web", Target: "https://a", Settings: checks.CheckSettings{"http": map[string]any{}}},
			2: {ID: 2, Job: "dns", Target: "example.com", Settings: checks.CheckSettings{"dns": map[string]any{}}},
			3: {ID: 3, Job: "script", Target: "https://c", Settings: checks.CheckSettings{"scripted": map[string]any{}}},
		},
		namedErrors: map[string]string{"*": "unknown query"},
	}
	srv := newCheckServer(t, st)
	loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", smDatasourceUID: "sm-uid"}

	_, _, err := runChecksLoader(t, loader, false, "", "status", "-o", "json")
	require.Error(t, err)
	// Each failure line starts "<query>: ", so this counts lines, not mentions.
	assert.Equal(t, 1, strings.Count(err.Error(), "checks_latency: "), "error: %s", err)
	assert.Contains(t, err.Error(), "checks_reachability")
}

// TestChecksStatusFailsWhenReachabilityFails pins that losing reachability is an
// error even when the other queries answer: it is the only input to OK/FAILING,
// so without it every row would read NODATA with exit code 0.
func TestChecksStatusFailsWhenReachabilityFails(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1: {ID: 1, Job: "web", Target: "https://a", Settings: checks.CheckSettings{"http": map[string]any{}}},
		},
		namedFrames: map[string][]dataframe.Frame{
			"checks_probe_count": {smSeriesFrame("web", "https://a", 1.0)},
		},
		namedLatency: map[string][]dataframe.Frame{
			"http": {smSeriesFrame("web", "https://a", 0.25)},
		},
		namedErrors: map[string]string{"checks_reachability": "unknown query"},
	}
	srv := newCheckServer(t, st)
	loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", smDatasourceUID: "sm-uid"}

	stdout, _, err := runChecksLoader(t, loader, false, "", "status", "-o", "json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checks_reachability")
	assert.Contains(t, err.Error(), "unknown query")
	assert.Empty(t, stdout, "no document should be emitted when status cannot be computed")
}

// TestChecksStatusWarnsOnPartialFailure pins that when only some queries fail the
// command still lists the checks, names each failed query on stderr, and keeps
// the warning off stdout.
func TestChecksStatusWarnsOnPartialFailure(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1: {ID: 1, Job: "web", Target: "https://a", Settings: checks.CheckSettings{"http": map[string]any{}}},
		},
		namedFrames: map[string][]dataframe.Frame{
			"checks_reachability": {smSeriesFrame("web", "https://a", 0.99)},
			"checks_probe_count":  {smSeriesFrame("web", "https://a", 1.0)},
		},
		namedErrors: map[string]string{"checks_latency": "unknown query"},
	}
	srv := newCheckServer(t, st)
	loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", smDatasourceUID: "sm-uid"}

	stdout, stderr, err := runChecksLoader(t, loader, false, "", "status", "-o", "json")
	require.NoError(t, err)

	docs, ok := decodeSingleJSONValue(t, stdout).([]any)
	require.True(t, ok)
	require.Len(t, docs, 1)
	row, ok := docs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "OK", row["status"])
	assert.NotContains(t, row, "latencyMs")

	assert.Contains(t, stderr, "checks_latency")
	assert.Contains(t, stderr, "unknown query")
	assert.NotContains(t, stderr, "checks_reachability")
}

// TestChecksStatusWarnsOncePerDistinctFailure pins that latency, queried once per
// check type, does not repeat the same warning for every type.
func TestChecksStatusWarnsOncePerDistinctFailure(t *testing.T) {
	st := &checkAPIState{
		probesOnline: true,
		checks: map[int64]checks.Check{
			1: {ID: 1, Job: "web", Target: "https://a", Settings: checks.CheckSettings{"http": map[string]any{}}},
			2: {ID: 2, Job: "dns", Target: "example.com", Settings: checks.CheckSettings{"dns": map[string]any{}}},
			3: {ID: 3, Job: "script", Target: "https://c", Settings: checks.CheckSettings{"scripted": map[string]any{}}},
		},
		namedFrames: map[string][]dataframe.Frame{
			"checks_reachability": {smSeriesFrame("web", "https://a", 1.0)},
		},
		namedErrors: map[string]string{"checks_latency": "unknown query"},
	}
	srv := newCheckServer(t, st)
	loader := &contractStatusLoader{baseURL: srv.URL, namespace: "default", smDatasourceUID: "sm-uid"}

	_, stderr, err := runChecksLoader(t, loader, false, "", "status", "-o", "json")
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(stderr, "status column unavailable"), "stderr: %s", stderr)
	assert.Contains(t, stderr, "checks_latency")
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
