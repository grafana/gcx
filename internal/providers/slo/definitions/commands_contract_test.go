package definitions_test

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
	"github.com/grafana/gcx/internal/providers/slo/definitions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// These tests pin the agent output contract for the slo definitions mutation
// commands (push, pull, delete) and the status/timeline empty paths:
//   - agent mode emits exactly one JSON value on stdout;
//   - the human default output stays byte-identical to the pre-codec lines;
//   - partial failures return *gcxerrors.EmittedError with ExitPartialFailure;
//   - explicit -o json/yaml overrides are honored.

type fakeGrafanaLoader struct{ url string }

func (f *fakeGrafanaLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	return config.NamespacedRESTConfig{Config: rest.Config{Host: f.url}, Namespace: "default"}, nil
}

// sloAPIState drives the fake SLO plugin API.
type sloAPIState struct {
	mu             sync.Mutex
	createCalls    int
	failCreateFrom int // fail POST create calls numbered >= this (1-based); 0 = never
	slos           map[string]definitions.Slo
	failDelete     map[string]bool
}

const sloBasePath = "/api/plugins/grafana-slo-app/resources/v1/slo"

func newSLOServer(t *testing.T, st *sloAPIState) *httptest.Server {
	t.Helper()
	if st.slos == nil {
		st.slos = map[string]definitions.Slo{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc(sloBasePath, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			st.mu.Lock()
			slos := make([]definitions.Slo, 0, len(st.slos))
			for _, s := range st.slos {
				slos = append(slos, s)
			}
			st.mu.Unlock()
			sort.Slice(slos, func(i, j int) bool { return slos[i].UUID < slos[j].UUID })
			writeJSON(w, definitions.SLOListResponse{SLOs: slos})
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
			var slo definitions.Slo
			_ = json.NewDecoder(r.Body).Decode(&slo)
			uuid := fmt.Sprintf("uuid-%d", n)
			slo.UUID = uuid
			st.mu.Lock()
			st.slos[uuid] = slo
			st.mu.Unlock()
			writeJSON(w, definitions.SLOCreateResponse{UUID: uuid})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc(sloBasePath+"/", func(w http.ResponseWriter, r *http.Request) {
		uuid := strings.TrimPrefix(r.URL.Path, sloBasePath+"/")
		switch r.Method {
		case http.MethodGet:
			st.mu.Lock()
			s, ok := st.slos[uuid]
			st.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			writeJSON(w, s)
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

// runDefinitions executes a `slo definitions` subcommand against the fake
// server, capturing stdout and stderr. The command tree is built after the
// agent flag is set, mirroring the real CLI (BindFlags reads agent mode at
// construction time).
func runDefinitions(t *testing.T, srvURL string, agentMode bool, stdin string, args ...string) (string, string, error) {
	t.Helper()
	prevNoColor := color.NoColor
	color.NoColor = true
	agent.SetFlag(agentMode)
	t.Cleanup(func() {
		agent.SetFlag(false)
		color.NoColor = prevNoColor
	})

	root := definitions.Commands(&fakeGrafanaLoader{url: srvURL})
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

func writeSLOManifest(t *testing.T, dir, file, name, uuid string) string {
	t.Helper()
	content := "apiVersion: slo.ext.grafana.app/v1alpha1\nkind: SLO\n"
	if uuid != "" {
		content += "metadata:\n  name: " + uuid + "\n"
	} else {
		content += "metadata: {}\n"
	}
	content += "spec:\n  name: \"" + name + "\"\n"
	path := filepath.Join(dir, file)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestDefinitionsPushOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		extraArgs  []string
		wantStdout string // exact match when set
		checkJSON  bool
		wantInOut  string // substring match when set
	}{
		{
			name:       "human default byte-identical",
			wantStdout: "✔ Created SLO One (uuid=uuid-1)\n✔ Updated SLO Two\n",
		},
		{
			name:       "human dry-run byte-identical",
			extraArgs:  []string{"--dry-run"},
			wantStdout: "🛈 [dry-run] Would push SLO \"SLO One\" (uuid=)\n🛈 [dry-run] Would push SLO \"SLO Two\" (uuid=uuid-2)\n",
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
			st := &sloAPIState{slos: map[string]definitions.Slo{
				"uuid-2": {UUID: "uuid-2", Name: "SLO Two"},
			}}
			srv := newSLOServer(t, st)

			dir := t.TempDir()
			f1 := writeSLOManifest(t, dir, "one.yaml", "SLO One", "")
			f2 := writeSLOManifest(t, dir, "two.yaml", "SLO Two", "uuid-2")

			args := append([]string{"push", f1, f2}, tc.extraArgs...)
			stdout, _, err := runDefinitions(t, srv.URL, tc.agentMode, "", args...)
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

func TestDefinitionsPushPartialFailure(t *testing.T) {
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
			st := &sloAPIState{failCreateFrom: 2}
			srv := newSLOServer(t, st)

			dir := t.TempDir()
			f1 := writeSLOManifest(t, dir, "one.yaml", "SLO One", "")
			f2 := writeSLOManifest(t, dir, "two.yaml", "SLO Two", "")

			stdout, stderr, err := runDefinitions(t, srv.URL, tc.agentMode, "", "push", f1, f2)

			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted, "partial failure must return EmittedError")
			assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			assert.Contains(t, stderr, "failed to create SLO")

			if tc.agentMode {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.slo.push_batch", doc["type"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 1, jsonInt(t, summary["succeeded"]))
				assert.Equal(t, 1, jsonInt(t, summary["failed"]))
				failures, ok := doc["failures"].([]any)
				require.True(t, ok)
				require.Len(t, failures, 1)
			} else {
				// Human stdout keeps only the pre-failure success lines,
				// exactly as before; the failure stays on stderr.
				assert.Equal(t, "✔ Created SLO One (uuid=uuid-1)\n", stdout)
			}
		})
	}
}

func TestDefinitionsPushTotalFailureUsesStandardErrorPath(t *testing.T) {
	st := &sloAPIState{failCreateFrom: 1}
	srv := newSLOServer(t, st)

	dir := t.TempDir()
	f1 := writeSLOManifest(t, dir, "one.yaml", "SLO One", "")

	stdout, _, err := runDefinitions(t, srv.URL, true, "", "push", f1)

	require.Error(t, err)
	var emitted *gcxerrors.EmittedError
	assert.NotErrorAs(t, err, &emitted, "total failure must not be EmittedError — no partial result was written")
	assert.Empty(t, stdout, "total failure must leave stdout to the standard error path")
}

func TestDefinitionsDeleteOutputContract(t *testing.T) {
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
			st := &sloAPIState{slos: map[string]definitions.Slo{
				"uuid-a": {UUID: "uuid-a", Name: "A"},
				"uuid-b": {UUID: "uuid-b", Name: "B"},
			}}
			srv := newSLOServer(t, st)

			args := append([]string{"delete", "uuid-a", "uuid-b", "--force"}, tc.extraArgs...)
			stdout, _, err := runDefinitions(t, srv.URL, tc.agentMode, "", args...)
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
				assert.Equal(t, "1", doc["schema_version"])
				assert.Equal(t, []any{"uuid-a", "uuid-b"}, doc["deleted"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 2, jsonInt(t, summary["succeeded"]))
			}
		})
	}
}

func TestDefinitionsDeletePartialFailure(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode"},
		{name: "agent mode", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &sloAPIState{
				slos: map[string]definitions.Slo{
					"uuid-a": {UUID: "uuid-a", Name: "A"},
					"uuid-b": {UUID: "uuid-b", Name: "B"},
				},
				failDelete: map[string]bool{"uuid-b": true},
			}
			srv := newSLOServer(t, st)

			stdout, stderr, err := runDefinitions(t, srv.URL, tc.agentMode, "", "delete", "uuid-a", "uuid-b", "--force")

			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			assert.Contains(t, stderr, "failed to delete SLO uuid-b")

			if tc.agentMode {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.slo.delete_batch", doc["type"])
				assert.Equal(t, []any{"uuid-a"}, doc["deleted"])
				summary, ok := doc["summary"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, 1, jsonInt(t, summary["succeeded"]))
				assert.Equal(t, 1, jsonInt(t, summary["failed"]))
			} else {
				assert.Equal(t, "✔ Deleted uuid-a\n", stdout)
			}
		})
	}
}

func TestDefinitionsDeletePromptOnStderr(t *testing.T) {
	st := &sloAPIState{slos: map[string]definitions.Slo{"uuid-a": {UUID: "uuid-a"}}}
	srv := newSLOServer(t, st)

	// Declined prompt: exit nil, nothing on stdout, prompt + note on stderr.
	stdout, stderr, err := runDefinitions(t, srv.URL, false, "n\n", "delete", "uuid-a")
	require.NoError(t, err)
	assert.Empty(t, stdout, "prompt and decline note must not touch stdout")
	assert.Contains(t, stderr, "Delete 1 SLO definition(s)? [y/N]")
	assert.Contains(t, stderr, "Aborted.")
}

func TestDefinitionsPullReceiptContract(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human mode byte-identical"},
		{name: "agent mode single JSON receipt", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &sloAPIState{slos: map[string]definitions.Slo{
				"uuid-a": {UUID: "uuid-a", Name: "A"},
				"uuid-b": {UUID: "uuid-b", Name: "B"},
			}}
			srv := newSLOServer(t, st)

			dir := t.TempDir()
			stdout, _, err := runDefinitions(t, srv.URL, tc.agentMode, "", "pull", "-d", dir)
			require.NoError(t, err)

			outputDir := filepath.Join(dir, "SLO")
			for _, uuid := range []string{"uuid-a", "uuid-b"} {
				assert.FileExists(t, filepath.Join(outputDir, uuid+".yaml"))
			}

			if tc.agentMode {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "gcx.artifact_receipt", doc["type"])
				assert.Equal(t, "yaml", doc["format"])
				files, ok := doc["files"].([]any)
				require.True(t, ok)
				require.Len(t, files, 1)
				entry, ok := files[0].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, outputDir, entry["path"])
				assert.Equal(t, 2, jsonInt(t, entry["count"]))
			} else {
				assert.Equal(t, "✔ Pulled 2 SLO definitions to "+outputDir+"/\n", stdout)
			}
		})
	}
}

func TestDefinitionsStatusEmptyContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		wantStdout string
		checkEmpty bool
	}{
		{name: "human default keeps prose notice", wantStdout: "🛈 No SLO definitions found.\n"},
		{name: "agent mode emits one empty JSON document", agentMode: true, checkEmpty: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSLOServer(t, &sloAPIState{})

			stdout, _, err := runDefinitions(t, srv.URL, tc.agentMode, "", "status")
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

func TestDefinitionsTimelineEmptyContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		wantStdout string
	}{
		{name: "human default keeps prose notice", wantStdout: "🛈 No SLO definitions found.\n"},
		{name: "agent mode emits one JSON document", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSLOServer(t, &sloAPIState{})

			stdout, _, err := runDefinitions(t, srv.URL, tc.agentMode, "", "timeline")
			require.NoError(t, err)

			if tc.agentMode {
				doc, ok := decodeSingleJSONValue(t, stdout).(map[string]any)
				require.True(t, ok, "timeline payload must be a JSON object, got: %q", stdout)
				assert.Contains(t, doc, "SLOs")
				assert.Contains(t, doc, "Points")
			} else {
				assert.Equal(t, tc.wantStdout, stdout)
			}
		})
	}
}
