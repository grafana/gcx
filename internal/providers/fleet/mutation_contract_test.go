package fleet //nolint:testpackage // Drives the unexported command constructors through the fleetHelper loader seam.

// These tests pin the pre-GA agent output contract for the six fleet mutation
// commands (pipelines/collectors x create/update/delete):
//
//   - default human stdout stays byte-identical to the pre-migration
//     cmdio.Success one-liner;
//   - agent mode emits EXACTLY ONE JSON value (a gcx.mutation document) on
//     stdout, then EOF;
//   - explicit -o json / -o yaml overrides are honored.
//
// The commands are driven end-to-end (cobra Execute) against a fake Fleet
// Management API server, with the cloud config loader stubbed through the
// CloudConfigLoader seam.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCloudLoader satisfies CloudConfigLoader, pointing the fleet base client
// at a test server.
type fakeCloudLoader struct{ url string }

func (f *fakeCloudLoader) LoadCloudConfig(context.Context) (providers.CloudRESTConfig, error) {
	return providers.CloudRESTConfig{
		Token: "test-token",
		Stack: cloud.StackInfo{
			AgentManagementInstanceURL: f.url,
			AgentManagementInstanceID:  42,
		},
		Namespace: "stack-1",
	}, nil
}

// newFleetAPIServer serves the connect-style Fleet Management endpoints the
// mutation commands hit, with one fixed pipeline (web-pipe/101) and one fixed
// collector (col-a/202).
func newFleetAPIServer(t *testing.T) *httptest.Server {
	t.Helper()

	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(v); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/pipeline.v1.PipelineService/CreatePipeline", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": "101", "name": "web-pipe"})
	})
	mux.HandleFunc("/pipeline.v1.PipelineService/GetPipeline", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": "101", "name": "web-pipe"})
	})
	mux.HandleFunc("/pipeline.v1.PipelineService/ListPipelines", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"pipelines": []map[string]any{{"id": "101", "name": "web-pipe"}}})
	})
	mux.HandleFunc("/pipeline.v1.PipelineService/UpdatePipeline", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{})
	})
	mux.HandleFunc("/pipeline.v1.PipelineService/DeletePipeline", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{})
	})
	mux.HandleFunc("/collector.v1.CollectorService/CreateCollector", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": "202", "name": "col-a"})
	})
	mux.HandleFunc("/collector.v1.CollectorService/GetCollector", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"id": "202", "name": "col-a"})
	})
	mux.HandleFunc("/collector.v1.CollectorService/ListCollectors", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"collectors": []map[string]any{{"id": "202", "name": "col-a"}}})
	})
	mux.HandleFunc("/collector.v1.CollectorService/UpdateCollector", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{})
	})
	mux.HandleFunc("/collector.v1.CollectorService/DeleteCollector", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func writeManifest(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func pipelineManifest(t *testing.T) string {
	t.Helper()
	return writeManifest(t, "pipeline.yaml", `apiVersion: fleet.ext.grafana.app/v1alpha1
kind: Pipeline
metadata:
  name: web-pipe
spec:
  name: web-pipe
  contents: "logging {}"
`)
}

func collectorManifest(t *testing.T) string {
	t.Helper()
	return writeManifest(t, "collector.yaml", `apiVersion: fleet.ext.grafana.app/v1alpha1
kind: Collector
metadata:
  name: col-a
spec:
  name: col-a
  collector_type: alloy
`)
}

// fleetMutationCases is the shared table for all six fleet mutation commands.
func fleetMutationCases(t *testing.T) []struct {
	name       string
	build      func(h *fleetHelper) *cobra.Command
	args       []string
	wantHuman  string
	wantAction string
	wantKind   string
	wantName   string
	wantID     string
} {
	t.Helper()
	return []struct {
		name       string
		build      func(h *fleetHelper) *cobra.Command
		args       []string
		wantHuman  string
		wantAction string
		wantKind   string
		wantName   string
		wantID     string
	}{
		{
			name:       "pipelines create",
			build:      func(h *fleetHelper) *cobra.Command { return h.newPipelineCreateCommand() },
			args:       []string{"-f", pipelineManifest(t)},
			wantHuman:  "✔ Created pipeline web-pipe (id=101)\n",
			wantAction: "created", wantKind: PipelineKind, wantName: "web-pipe", wantID: "101",
		},
		{
			name:       "pipelines update",
			build:      func(h *fleetHelper) *cobra.Command { return h.newPipelineUpdateCommand() },
			args:       []string{"web-pipe-101", "-f", pipelineManifest(t)},
			wantHuman:  "✔ Updated pipeline web-pipe-101\n",
			wantAction: "updated", wantKind: PipelineKind, wantName: "web-pipe-101", wantID: "101",
		},
		{
			name:       "pipelines delete",
			build:      func(h *fleetHelper) *cobra.Command { return h.newPipelineDeleteCommand() },
			args:       []string{"web-pipe-101"},
			wantHuman:  "✔ Deleted pipeline web-pipe-101\n",
			wantAction: "deleted", wantKind: PipelineKind, wantName: "web-pipe-101", wantID: "101",
		},
		{
			name:       "collectors create",
			build:      func(h *fleetHelper) *cobra.Command { return h.newCollectorCreateCommand() },
			args:       []string{"-f", collectorManifest(t)},
			wantHuman:  "✔ Created collector col-a (id=202)\n",
			wantAction: "created", wantKind: CollectorKind, wantName: "col-a", wantID: "202",
		},
		{
			name:       "collectors update",
			build:      func(h *fleetHelper) *cobra.Command { return h.newCollectorUpdateCommand() },
			args:       []string{"202", "-f", collectorManifest(t)},
			wantHuman:  "✔ Updated collector 202\n",
			wantAction: "updated", wantKind: CollectorKind, wantName: "col-a", wantID: "202",
		},
		{
			name:       "collectors delete",
			build:      func(h *fleetHelper) *cobra.Command { return h.newCollectorDeleteCommand() },
			args:       []string{"202"},
			wantHuman:  "✔ Deleted collector 202\n",
			wantAction: "deleted", wantKind: CollectorKind, wantName: "", wantID: "202",
		},
	}
}

// runCommand builds the command against a fresh fake API server and executes
// it, capturing stdout.
func runCommand(t *testing.T, build func(h *fleetHelper) *cobra.Command, args []string) (string, error) {
	t.Helper()
	server := newFleetAPIServer(t)
	h := &fleetHelper{loader: &fakeCloudLoader{url: server.URL}}
	cmd := build(h)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), err
}

func withPlainColors(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = prev })
}

// decodeSingleJSONValue asserts that raw holds exactly one JSON value
// followed by EOF, and returns it.
func decodeSingleJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var doc map[string]any
	require.NoError(t, dec.Decode(&doc), "stdout is not valid JSON:\n%s", raw)
	var second any
	err := dec.Decode(&second)
	require.ErrorIs(t, err, io.EOF, "stdout must contain exactly one JSON value\n%s", raw)
	return doc
}

func TestFleetMutations_HumanDefault_ByteIdentical(t *testing.T) {
	withPlainColors(t)
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	for _, tc := range fleetMutationCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			stdout, err := runCommand(t, tc.build, tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.wantHuman, stdout, "default human stdout must stay byte-identical")
		})
	}
}

func TestFleetMutations_AgentMode_SingleJSONDocument(t *testing.T) {
	withPlainColors(t)

	for _, tc := range fleetMutationCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			// The agents default is resolved when the command binds its
			// flags, so the flag must be set before building the command.
			agent.SetFlag(true)
			t.Cleanup(func() { agent.SetFlag(false) })

			stdout, err := runCommand(t, tc.build, tc.args)
			require.NoError(t, err)

			doc := decodeSingleJSONValue(t, stdout)
			assert.Equal(t, "gcx.mutation", doc["type"])
			assert.Equal(t, "1", doc["schema_version"])
			assert.Equal(t, tc.wantAction, doc["action"])

			target, ok := doc["target"].(map[string]any)
			require.True(t, ok, "target must be an object: %v", doc["target"])
			assert.Equal(t, tc.wantKind, target["kind"])
			assert.Equal(t, tc.wantID, target["id"])
			if tc.wantName != "" {
				assert.Equal(t, tc.wantName, target["name"])
			}
		})
	}
}

func TestFleetMutations_ExplicitOutputOverride(t *testing.T) {
	withPlainColors(t)
	agent.SetFlag(false)
	t.Cleanup(func() { agent.SetFlag(false) })

	for _, tc := range fleetMutationCases(t) {
		t.Run(tc.name+" -o json", func(t *testing.T) {
			stdout, err := runCommand(t, tc.build, append(tc.args, "-o", "json"))
			require.NoError(t, err)
			doc := decodeSingleJSONValue(t, stdout)
			assert.Equal(t, "gcx.mutation", doc["type"])
			assert.Equal(t, tc.wantAction, doc["action"])
		})

		t.Run(tc.name+" -o yaml", func(t *testing.T) {
			stdout, err := runCommand(t, tc.build, append(tc.args, "-o", "yaml"))
			require.NoError(t, err)
			assert.Contains(t, stdout, "type: gcx.mutation")
			assert.Contains(t, stdout, "action: "+tc.wantAction)
			assert.NotContains(t, stdout, "✔", "explicit -o yaml must not carry the styled human line")
		})
	}
}
