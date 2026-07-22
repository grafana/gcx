package irm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/irm"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
)

// setAgentModeExt flips agent-mode detection for the duration of the test.
// Commands must be constructed AFTER calling this — BindFlags reads the
// agent-mode state at construction time.
func setAgentModeExt(t *testing.T, on bool) {
	t.Helper()
	if on {
		t.Setenv("GCX_AGENT_MODE", "true")
	} else {
		t.Setenv("GCX_AGENT_MODE", "false")
	}
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}

// decodeOneJSONValue asserts raw is exactly one JSON value followed by EOF
// and returns the decoded document.
func decodeOneJSONValue(t *testing.T, raw string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not a JSON value: %v\nstdout=%q", err, raw)
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		t.Fatalf("expected exactly one JSON value then EOF on stdout; second decode returned %v\nstdout=%q", err, raw)
	}
	return doc
}

// runIncidentCmd executes a freshly-built incidents command with separated
// stdout/stderr streams.
func runIncidentCmd(t *testing.T, build func() *cobra.Command, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := build()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

// incidentAPIServer serves the IRM plugin API surface the migrated commands
// hit, keyed by the RPC method suffix in the URL.
func incidentAPIServer(t *testing.T, responses map[string]any) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for suffix, body := range responses {
			if strings.HasSuffix(r.URL.Path, suffix) {
				writeJSON(w, body)
				return
			}
		}
		t.Errorf("unexpected API call: %s", r.URL.Path)
		http.Error(w, "unexpected call", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	return server
}

func incidentLoader(server *httptest.Server) fakeGrafanaConfigLoader {
	return fakeGrafanaConfigLoader{cfg: config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "stack-123",
	}}
}

// TestIncidentsCloseOutputContract pins the agent output contract for
// `irm incidents close`.
func TestIncidentsCloseOutputContract(t *testing.T) {
	closedIncident := map[string]any{
		"incident": map[string]any{
			"incidentID": "inc-1",
			"title":      "Database down",
			"status":     "resolved",
		},
	}

	tests := []struct {
		name       string
		agentMode  bool
		args       []string
		wantStdout string // exact stdout when wantJSON is nil
		wantJSON   bool
	}{
		{
			name:       "human default is the byte-identical prose line",
			args:       []string{"inc-1"},
			wantStdout: "✔ Closed incident inc-1 (Database down)\n",
		},
		{
			name:      "agent mode emits exactly one JSON value",
			agentMode: true,
			args:      []string{"inc-1"},
			wantJSON:  true,
		},
		{
			name:     "explicit -o json wins outside agent mode",
			args:     []string{"inc-1", "-o", "json"},
			wantJSON: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentModeExt(t, tc.agentMode)
			server := incidentAPIServer(t, map[string]any{
				"IncidentsService.UpdateStatus": closedIncident,
			})

			stdout, _, err := runIncidentCmd(t, func() *cobra.Command {
				return irm.NewCloseCommand(incidentLoader(server))
			}, "", tc.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantJSON {
				doc := decodeOneJSONValue(t, stdout)
				if doc["type"] != "gcx.mutation" || doc["schema_version"] != "1" || doc["action"] != "closed" {
					t.Errorf("unexpected result envelope: %v", doc)
				}
				target, _ := doc["target"].(map[string]any)
				if target["kind"] != "Incident" || target["id"] != "inc-1" || target["name"] != "Database down" {
					t.Errorf("target = %v, want kind=Incident id=inc-1 name=Database down", target)
				}
				if doc["changed"] != true {
					t.Errorf("changed = %v, want true", doc["changed"])
				}
				return
			}
			if stdout != tc.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tc.wantStdout)
			}
		})
	}
}

// TestIncidentsActivityAddOutputContract pins the agent output contract for
// `irm incidents activity add`.
func TestIncidentsActivityAddOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		args       []string
		wantStdout string
		wantJSON   bool
	}{
		{
			name:       "human default is the byte-identical prose line",
			args:       []string{"add", "inc-9", "--body", "note"},
			wantStdout: "✔ Added activity note to incident inc-9\n",
		},
		{
			name:      "agent mode emits exactly one JSON value",
			agentMode: true,
			args:      []string{"add", "inc-9", "--body", "note"},
			wantJSON:  true,
		},
		{
			name:     "explicit -o json wins outside agent mode",
			args:     []string{"add", "inc-9", "--body", "note", "-o", "json"},
			wantJSON: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentModeExt(t, tc.agentMode)
			server := incidentAPIServer(t, map[string]any{
				"ActivityService.AddActivity": map[string]any{},
			})

			stdout, _, err := runIncidentCmd(t, func() *cobra.Command {
				return irm.NewActivityCommand(incidentLoader(server))
			}, "", tc.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantJSON {
				doc := decodeOneJSONValue(t, stdout)
				if doc["type"] != "gcx.mutation" || doc["action"] != "add-activity" {
					t.Errorf("unexpected result envelope: %v", doc)
				}
				target, _ := doc["target"].(map[string]any)
				if target["kind"] != "Incident" || target["id"] != "inc-9" {
					t.Errorf("target = %v, want kind=Incident id=inc-9", target)
				}
				return
			}
			if stdout != tc.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tc.wantStdout)
			}
		})
	}
}

const createManifest = `apiVersion: incident.ext.grafana.app/v1alpha1
kind: Incident
metadata:
  name: my-incident
spec:
  title: "Boom"
  status: active
`

// TestIncidentsCreateOutputContract pins the contamination fix for
// `irm incidents create`: the echoed incident stays the stdout result (yaml
// by default, one JSON value in agent mode) and the confirmation one-liner
// moves to stderr.
func TestIncidentsCreateOutputContract(t *testing.T) {
	createdIncident := map[string]any{
		"incident": map[string]any{
			"incidentID": "inc-42",
			"title":      "Boom",
			"status":     "active",
		},
	}

	tests := []struct {
		name      string
		agentMode bool
	}{
		{name: "human default: yaml echo on stdout, prose on stderr"},
		{name: "agent mode: exactly one JSON value on stdout, prose on stderr", agentMode: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentModeExt(t, tc.agentMode)
			server := incidentAPIServer(t, map[string]any{
				"IncidentsService.CreateIncident": createdIncident,
			})

			stdout, stderr, err := runIncidentCmd(t, func() *cobra.Command {
				return irm.NewCreateCommand(incidentLoader(server))
			}, createManifest, "-f", "-")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(stderr, "Created incident Boom (id=inc-42)") {
				t.Errorf("expected confirmation prose on stderr; stderr=%q", stderr)
			}
			if strings.Contains(stdout, "Created incident") {
				t.Errorf("confirmation prose must not contaminate stdout; stdout=%q", stdout)
			}

			if tc.agentMode {
				doc := decodeOneJSONValue(t, stdout)
				metadata, _ := doc["metadata"].(map[string]any)
				if metadata["name"] != "inc-42" {
					t.Errorf("expected echoed incident envelope with metadata.name=inc-42; got %v", doc)
				}
				return
			}
			if !strings.HasPrefix(stdout, "apiVersion:") {
				t.Errorf("human default must keep the yaml echo on stdout; stdout=%q", stdout)
			}
			if !strings.Contains(stdout, "inc-42") {
				t.Errorf("echoed incident must carry the created ID; stdout=%q", stdout)
			}
		})
	}
}

// TestIncidentsListEmptyEncodesEmptyArray pins that an empty incident list
// encodes as [] (never null) for structured formats.
func TestIncidentsListEmptyEncodesEmptyArray(t *testing.T) {
	emptyPage := map[string]any{
		"incidentPreviews": []map[string]any{},
		"cursor":           map[string]any{"hasMore": false},
	}

	tests := []struct {
		name      string
		agentMode bool
		args      []string
	}{
		{
			name: "explicit -o json",
			args: []string{"-o", "json"},
		},
		{
			name:      "agent mode default",
			agentMode: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentModeExt(t, tc.agentMode)
			server := incidentAPIServer(t, map[string]any{
				"IncidentsService.QueryIncidentPreviews": emptyPage,
			})

			stdout, _, err := runIncidentCmd(t, func() *cobra.Command {
				return irm.NewListCommand(incidentLoader(server))
			}, "", tc.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			dec := json.NewDecoder(strings.NewReader(stdout))
			var arr []any
			if err := dec.Decode(&arr); err != nil {
				t.Fatalf("stdout is not a JSON array: %v\nstdout=%q", err, stdout)
			}
			if arr == nil || len(arr) != 0 {
				t.Errorf("expected empty (non-null) array, got %v (stdout=%q)", arr, stdout)
			}
			if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
				t.Fatalf("expected exactly one JSON value then EOF; second decode returned %v", err)
			}
		})
	}
}
