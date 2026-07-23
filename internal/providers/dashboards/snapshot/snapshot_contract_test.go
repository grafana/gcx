package snapshot_test

// Contract tests for the `dashboards snapshot` migration to the pre-GA agent
// output contract:
//   - default human stdout stays byte-identical (the NAME/PANEL/FILE/SIZE
//     table of successful renders);
//   - agent mode emits exactly ONE JSON value — the gcx.dashboards.snapshot
//     receipt — even on partial failure (previously the success array and a
//     second error document were both written to stdout);
//   - partial failure returns *gcxerrors.EmittedError with ExitPartialFailure;
//   - explicit -o json/yaml overrides are honored.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/dashboards"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/dashboards/snapshot"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

var testPNG = []byte("\x89PNG\r\n\x1a\nfake-pixels") //nolint:gochecknoglobals

// stubLoader satisfies snapshot.GrafanaConfigLoader with a fixed config.
type stubLoader struct {
	cfg config.NamespacedRESTConfig
}

func (s *stubLoader) LoadGrafanaConfig(context.Context) (config.NamespacedRESTConfig, error) {
	return s.cfg, nil
}

// newRenderServer serves testPNG for every render request except UIDs
// containing "bad", which get a 500.
func newRenderServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "bad") {
			http.Error(w, "render backend exploded", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(testPNG)
	}))
	t.Cleanup(server.Close)
	return server
}

// runSnapshotCmd builds the snapshot command against the given server and
// executes it. Agent mode must be pinned BEFORE calling this: the codec
// default is resolved when flags are bound.
func runSnapshotCmd(t *testing.T, server *httptest.Server, args []string) (string, string, error) {
	t.Helper()

	loader := &stubLoader{cfg: config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}}

	cmd := snapshot.Commands(loader)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)

	err := cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// decodeSingleJSONDoc asserts stdout holds exactly one JSON value (then EOF)
// and returns it as a map.
func decodeSingleJSONDoc(t *testing.T, stdout string) map[string]any {
	t.Helper()

	dec := json.NewDecoder(strings.NewReader(stdout))
	var first map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout)
	}
	var second any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout must contain exactly one JSON value, second decode = %v\n%s", err, stdout)
	}
	return first
}

func TestSnapshot_HumanDefaultByteIdentical(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)

	server := newRenderServer(t)
	outputDir := t.TempDir()

	stdout, _, err := runSnapshotCmd(t, server, []string{"dash-a", "--output-dir", outputDir})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}

	wantPath, err := filepath.Abs(filepath.Join(outputDir, "dash-a.png"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}

	// The PNG artifact must be on disk.
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read PNG artifact: %v", err)
	}
	if !bytes.Equal(data, testPNG) {
		t.Error("PNG artifact content mismatch")
	}

	// Byte-identical human stdout: exactly what renderSnapshotTable has
	// always produced for the successful results.
	var expected bytes.Buffer
	renderErr := snapshot.RenderSnapshotTableForTest(&expected, []dashboards.SnapshotResult{{
		UID:      "dash-a",
		FilePath: wantPath,
		Width:    1920,
		Height:   -1,
		Theme:    "dark",
		FileSize: int64(len(testPNG)),
	}})
	if renderErr != nil {
		t.Fatalf("render expected table: %v", renderErr)
	}
	if stdout != expected.String() {
		t.Errorf("stdout not byte-identical to the historical table\n got: %q\nwant: %q", stdout, expected.String())
	}
}

func TestSnapshot_AgentModeSingleReceiptDoc(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(agent.ResetForTesting)

	server := newRenderServer(t)
	outputDir := t.TempDir()

	stdout, _, err := runSnapshotCmd(t, server, []string{"dash-a", "--output-dir", outputDir})
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}

	doc := decodeSingleJSONDoc(t, stdout)
	if doc["type"] != "gcx.dashboards.snapshot" {
		t.Errorf("type = %v, want gcx.dashboards.snapshot", doc["type"])
	}
	if doc["schema_version"] != "1" {
		t.Errorf("schema_version = %v, want 1", doc["schema_version"])
	}
	if doc["action"] != "rendered" {
		t.Errorf("action = %v, want rendered", doc["action"])
	}
	if doc["format"] != "png" {
		t.Errorf("format = %v, want png", doc["format"])
	}

	files, ok := doc["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("files = %v, want one entry", doc["files"])
	}
	file, ok := files[0].(map[string]any)
	if !ok || file["uid"] != "dash-a" {
		t.Errorf("files[0] = %v, want uid dash-a", files[0])
	}

	summary, ok := doc["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary = %v, want object", doc["summary"])
	}
	if summary["succeeded"] != float64(1) || summary["failed"] != float64(0) {
		t.Errorf("summary = %v, want succeeded 1 / failed 0", summary)
	}

	failures, ok := doc["failures"].([]any)
	if !ok {
		t.Fatalf("failures = %v (%T), want present [] — never null", doc["failures"], doc["failures"])
	}
	if len(failures) != 0 {
		t.Errorf("failures = %v, want empty", failures)
	}
}

func TestSnapshot_PartialFailure_OneDocAndExit4(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "agent mode", agentMode: true},
		{name: "human mode", agentMode: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetFlag(tt.agentMode)
			t.Cleanup(agent.ResetForTesting)

			server := newRenderServer(t)
			outputDir := t.TempDir()

			stdout, stderr, err := runSnapshotCmd(t, server,
				[]string{"dash-a", "bad-dash", "--output-dir", outputDir})

			// The error must be an EmittedError carrying ExitPartialFailure:
			// the receipt on stdout is complete and reportError must not
			// append a second document.
			var emitted *gcxerrors.EmittedError
			if !errors.As(err, &emitted) {
				t.Fatalf("Execute() error = %T (%v), want *gcxerrors.EmittedError", err, err)
			}
			if emitted.Code != gcxerrors.ExitPartialFailure {
				t.Fatalf("EmittedError.Code = %d, want %d", emitted.Code, gcxerrors.ExitPartialFailure)
			}

			// Failure detail is surfaced on stderr (advisory stream).
			if !strings.Contains(stderr, "bad-dash") {
				t.Errorf("stderr = %q, want it to mention the failed dashboard", stderr)
			}

			if tt.agentMode {
				assertPartialFailureReceipt(t, stdout)
			} else {
				assertSuccessOnlyTable(t, stdout, outputDir)
			}
		})
	}
}

// assertPartialFailureReceipt asserts stdout carries exactly one JSON value —
// the receipt with the one success and the one enumerated failure. The
// single-decode check is the regression gate for the old double-document
// stdout (success array + reportError's second JSON value).
func assertPartialFailureReceipt(t *testing.T, stdout string) {
	t.Helper()

	doc := decodeSingleJSONDoc(t, stdout)

	files, ok := doc["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("files = %v, want the one successful render", doc["files"])
	}

	failures, ok := doc["failures"].([]any)
	if !ok || len(failures) != 1 {
		t.Fatalf("failures = %v, want one entry", doc["failures"])
	}
	failure, ok := failures[0].(map[string]any)
	if !ok {
		t.Fatalf("failures[0] = %v, want object", failures[0])
	}
	target, ok := failure["target"].(map[string]any)
	if !ok || target["name"] != "bad-dash" {
		t.Errorf("failures[0].target = %v, want name bad-dash", failure["target"])
	}

	summary, ok := doc["summary"].(map[string]any)
	if !ok || summary["succeeded"] != float64(1) || summary["failed"] != float64(1) {
		t.Errorf("summary = %v, want succeeded 1 / failed 1", doc["summary"])
	}
}

// assertSuccessOnlyTable asserts human stdout stays the successes-only table,
// byte-identical to the historical rendering.
func assertSuccessOnlyTable(t *testing.T, stdout, outputDir string) {
	t.Helper()

	wantPath, err := filepath.Abs(filepath.Join(outputDir, "dash-a.png"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	var expected bytes.Buffer
	if renderErr := snapshot.RenderSnapshotTableForTest(&expected, []dashboards.SnapshotResult{{
		UID:      "dash-a",
		FilePath: wantPath,
		Width:    1920,
		Height:   -1,
		Theme:    "dark",
		FileSize: int64(len(testPNG)),
	}}); renderErr != nil {
		t.Fatalf("render expected table: %v", renderErr)
	}
	if stdout != expected.String() {
		t.Errorf("stdout not byte-identical to the historical table\n got: %q\nwant: %q", stdout, expected.String())
	}
}

func TestSnapshot_AllFail_RawErrorNotPartial(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(agent.ResetForTesting)

	server := newRenderServer(t)
	outputDir := t.TempDir()

	stdout, _, err := runSnapshotCmd(t, server, []string{"bad-dash", "--output-dir", outputDir})

	// Zero successes is a total failure, not a partial one: no receipt on
	// stdout (a success-shaped document with zero files would mislead) and
	// the raw error takes the standard path — exit 1 via reportError, not
	// EmittedError exit 4. This matches the batch cohort's zero-success
	// convention (slo, synth, kg, metrics, traces).
	if err == nil {
		t.Fatal("Execute() = nil, want the render error")
	}
	var emitted *gcxerrors.EmittedError
	if errors.As(err, &emitted) {
		t.Fatalf("Execute() error = EmittedError (exit %d), want a raw error on total failure", emitted.Code)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("stdout = %q, want empty (reportError owns the error document)", stdout)
	}
}

func TestSnapshot_ExplicitFormatOverride(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
		format    string
	}{
		{name: "-o json human mode", format: "json"},
		{name: "-o yaml agent mode", agentMode: true, format: "yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetFlag(tt.agentMode)
			t.Cleanup(agent.ResetForTesting)

			server := newRenderServer(t)
			outputDir := t.TempDir()

			stdout, _, err := runSnapshotCmd(t, server,
				[]string{"dash-a", "--output-dir", outputDir, "-o", tt.format})
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}

			var doc map[string]any
			switch tt.format {
			case "json":
				doc = decodeSingleJSONDoc(t, stdout)
			case "yaml":
				if yamlErr := yaml.Unmarshal([]byte(stdout), &doc); yamlErr != nil {
					t.Fatalf("stdout is not valid YAML: %v\n%s", yamlErr, stdout)
				}
			}

			if doc["type"] != "gcx.dashboards.snapshot" {
				t.Errorf("type = %v, want gcx.dashboards.snapshot\n%s", doc["type"], stdout)
			}
			if doc["schema_version"] != "1" {
				t.Errorf("schema_version = %v, want 1", doc["schema_version"])
			}
		})
	}
}
