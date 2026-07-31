package reports_test

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
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/slo/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// These tests pin the agent output contract for the slo reports mutation
// commands (push, pull, delete):
//   - agent mode emits exactly one JSON value on stdout;
//   - the human default output stays byte-identical to the pre-codec lines;
//   - partial failures return *gcxerrors.EmittedError with ExitPartialFailure;
//   - explicit -o json/yaml overrides are honored.

type fakeGrafanaLoader struct{ url string }

func (f *fakeGrafanaLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: f.url}, Namespace: "default"}, nil
}

// reportAPIState drives the fake SLO report API.
type reportAPIState struct {
	mu             sync.Mutex
	createCalls    int
	failCreateFrom int // fail POST create calls numbered >= this (1-based); 0 = never
	reports        map[string]reports.Report
	failDelete     map[string]bool
}

const reportBasePath = "/api/plugins/grafana-slo-app/resources/v1/report"

func newReportServer(t *testing.T, st *reportAPIState) *httptest.Server {
	t.Helper()
	if st.reports == nil {
		st.reports = map[string]reports.Report{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(reportBasePath, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			st.mu.Lock()
			rpts := make([]reports.Report, 0, len(st.reports))
			for _, s := range st.reports {
				rpts = append(rpts, s)
			}
			st.mu.Unlock()
			sort.Slice(rpts, func(i, j int) bool { return rpts[i].UUID < rpts[j].UUID })
			writeJSON(w, reports.ReportListResponse{Reports: rpts})
		case http.MethodPost:
			st.mu.Lock()
			st.createCalls++
			n := st.createCalls
			st.mu.Unlock()
			if st.failCreateFrom > 0 && n >= st.failCreateFrom {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "boom"})
				return
			}
			var rpt reports.Report
			_ = json.NewDecoder(r.Body).Decode(&rpt)
			uuid := fmt.Sprintf("uuid-%d", n)
			rpt.UUID = uuid
			st.mu.Lock()
			st.reports[uuid] = rpt
			st.mu.Unlock()
			writeJSON(w, reports.ReportCreateResponse{UUID: uuid})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(reportBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		uuid := strings.TrimPrefix(r.URL.Path, reportBasePath+"/")
		switch r.Method {
		case http.MethodGet:
			st.mu.Lock()
			rpt, ok := st.reports[uuid]
			st.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeJSON(w, rpt)
		case http.MethodPut:
			_, _ = w.Write([]byte("{}"))
		case http.MethodDelete:
			if st.failDelete[uuid] {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "boom"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// runReports executes a `slo reports` subcommand against the fake server,
// capturing stdout and stderr. The command tree is built after the agent flag
// is set, mirroring the real CLI (BindFlags reads agent mode at construction
// time).
func runReports(t *testing.T, srvURL string, agentMode bool, stdin string, args ...string) (string, string, error) {
	t.Helper()
	prevNoColor := color.NoColor
	color.NoColor = true
	agent.SetFlag(agentMode)
	t.Cleanup(func() {
		agent.SetFlag(false)
		color.NoColor = prevNoColor
	})

	root := reports.Commands(&fakeGrafanaLoader{url: srvURL})
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

func writeReportManifest(t *testing.T, dir, file, name, uuid string) string {
	t.Helper()
	content := "apiVersion: slo.ext.grafana.app/v1alpha1\nkind: Report\n"
	if uuid != "" {
		content += "metadata:\n  name: " + uuid + "\n"
	} else {
		content += "metadata: {}\n"
	}
	content += "spec:\n  name: \"" + name + "\"\n  timeSpan: weeklySundayToSunday\n"
	path := filepath.Join(dir, file)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestReportsPushOutputContract(t *testing.T) {
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
			wantStdout: "✔ Created Report One (uuid=uuid-1)\n✔ Updated Report Two\n",
		},
		{
			name:       "human dry-run byte-identical",
			extraArgs:  []string{"--dry-run"},
			wantStdout: "🛈 [dry-run] Would push report \"Report One\" (uuid=)\n🛈 [dry-run] Would push report \"Report Two\" (uuid=uuid-2)\n",
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
			wantInOut: "type: gcx.slo.push_batch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &reportAPIState{reports: map[string]reports.Report{
				"uuid-2": {UUID: "uuid-2", Name: "Report Two"},
			}}
			srv := newReportServer(t, st)

			dir := t.TempDir()
			f1 := writeReportManifest(t, dir, "one.yaml", "Report One", "")
			f2 := writeReportManifest(t, dir, "two.yaml", "Report Two", "uuid-2")

			args := append([]string{"push", f1, f2}, tc.extraArgs...)
			stdout, _, err := runReports(t, srv.URL, tc.agentMode, "", args...)
			require.NoError(t, err)

			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
			}
			if tc.wantInOut != "" {
				assert.Contains(t, stdout, tc.wantInOut)
			}
			if tc.checkJSON {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok, "push result must be a JSON object")
				assert.Equal(t, "gcx.slo.push_batch", doc["type"])
				assert.Equal(t, "1", doc["schema_version"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 2, jsonInt(t, summary["succeeded"]))
				assert.Empty(t, doc["failures"])
			}
		})
	}
}

func TestReportsPushPartialFailure(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode"},
		{name: "agent mode", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Second create fails: file one succeeds, file two fails.
			st := &reportAPIState{failCreateFrom: 2}
			srv := newReportServer(t, st)

			dir := t.TempDir()
			f1 := writeReportManifest(t, dir, "one.yaml", "Report One", "")
			f2 := writeReportManifest(t, dir, "two.yaml", "Report Two", "")

			stdout, stderr, err := runReports(t, srv.URL, tc.agentMode, "", "push", f1, f2)

			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted, "partial failure must return EmittedError")
			assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			assert.Contains(t, stderr, "failed to create report")

			if tc.agentMode {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.slo.push_batch", doc["type"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 1, jsonInt(t, summary["succeeded"]))
				assert.Equal(t, 1, jsonInt(t, summary["failed"]))
			} else {
				assert.Equal(t, "✔ Created Report One (uuid=uuid-1)\n", stdout)
			}
		})
	}
}

func TestReportsDeleteOutputContract(t *testing.T) {
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
			wantStdout: "✔ Deleted uuid-a\n✔ Deleted uuid-b\n",
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
			wantInOut: "type: gcx.slo.delete_batch",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &reportAPIState{reports: map[string]reports.Report{
				"uuid-a": {UUID: "uuid-a", Name: "A"},
				"uuid-b": {UUID: "uuid-b", Name: "B"},
			}}
			srv := newReportServer(t, st)

			args := append([]string{"delete", "uuid-a", "uuid-b", "--force"}, tc.extraArgs...)
			stdout, _, err := runReports(t, srv.URL, tc.agentMode, "", args...)
			require.NoError(t, err)

			if tc.wantStdout != "" {
				assert.Equal(t, tc.wantStdout, stdout)
			}
			if tc.wantInOut != "" {
				assert.Contains(t, stdout, tc.wantInOut)
			}
			if tc.checkJSON {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.slo.delete_batch", doc["type"])
				assert.Equal(t, []any{"uuid-a", "uuid-b"}, doc["deleted"])
			}
		})
	}
}

func TestReportsDeletePartialFailure(t *testing.T) {
	st := &reportAPIState{
		reports: map[string]reports.Report{
			"uuid-a": {UUID: "uuid-a", Name: "A"},
			"uuid-b": {UUID: "uuid-b", Name: "B"},
		},
		failDelete: map[string]bool{"uuid-b": true},
	}
	srv := newReportServer(t, st)

	stdout, stderr, err := runReports(t, srv.URL, true, "", "delete", "uuid-a", "uuid-b", "--force")

	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
	assert.Contains(t, stderr, "failed to delete report uuid-b")

	doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{"uuid-a"}, doc["deleted"])
	summary, ok := doc["summary"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 1, jsonInt(t, summary["succeeded"]))
	assert.Equal(t, 1, jsonInt(t, summary["failed"]))
}

func TestReportsDeletePromptOnStderr(t *testing.T) {
	st := &reportAPIState{reports: map[string]reports.Report{"uuid-a": {UUID: "uuid-a"}}}
	srv := newReportServer(t, st)

	stdout, stderr, err := runReports(t, srv.URL, false, "n\n", "delete", "uuid-a")
	require.NoError(t, err)
	assert.Empty(t, stdout, "prompt and decline note must not touch stdout")
	assert.Contains(t, stderr, "Delete 1 report(s)? [y/N]")
	assert.Contains(t, stderr, "Aborted.")
}

func TestReportsPullReceiptContract(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode byte-identical"},
		{name: "agent mode single JSON receipt", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &reportAPIState{reports: map[string]reports.Report{
				"uuid-a": {UUID: "uuid-a", Name: "A"},
			}}
			srv := newReportServer(t, st)

			dir := t.TempDir()
			stdout, _, err := runReports(t, srv.URL, tc.agentMode, "", "pull", "-d", dir)
			require.NoError(t, err)

			outputDir := filepath.Join(dir, "Report")
			assert.FileExists(t, filepath.Join(outputDir, "uuid-a.yaml"))

			if tc.agentMode {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.artifact_receipt", doc["type"])
				assert.Equal(t, "yaml", doc["format"])
			} else {
				assert.Equal(t, "✔ Pulled 1 SLO reports to "+outputDir+"/\n", stdout)
			}
		})
	}
}
