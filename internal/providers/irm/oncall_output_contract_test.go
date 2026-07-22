//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// decodeSingleJSONValue asserts raw contains exactly one JSON value followed
// by EOF (the agent output contract for finite commands) and returns it.
func decodeSingleJSONValue(t *testing.T, raw string) map[string]any {
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

// setAgentMode flips agent-mode detection for the duration of the test.
// Commands must be constructed AFTER calling this — BindFlags reads the
// agent-mode state at construction time.
func setAgentMode(t *testing.T, on bool) {
	t.Helper()
	if on {
		t.Setenv("GCX_AGENT_MODE", "true")
	} else {
		t.Setenv("GCX_AGENT_MODE", "false")
	}
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)
}

// fakeDeleteAPI stubs the delete surface for the nouns exercised below.
type fakeDeleteAPI struct {
	OnCallAPI

	deletedRoutes       []string
	deletedIntegrations []string
}

func (f *fakeDeleteAPI) DeleteRoute(_ context.Context, id string) error {
	f.deletedRoutes = append(f.deletedRoutes, id)
	return nil
}

func (f *fakeDeleteAPI) DeleteIntegration(_ context.Context, id string) error {
	f.deletedIntegrations = append(f.deletedIntegrations, id)
	return nil
}

// runNounCmd builds the given noun command tree and executes it with
// separated stdout/stderr streams.
func runNounCmd(t *testing.T, build func() *cobra.Command, stdin string, args ...string) (string, string, error) {
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

// TestOnCallCRUDDeleteOutputContract pins the agent output contract for the
// shared CRUD delete subcommand (all nine mutable OnCall nouns route through
// newDeleteSubcommand; routes and integrations are exercised as
// representatives of the label/kind wiring).
func TestOnCallCRUDDeleteOutputContract(t *testing.T) {
	tests := []struct {
		name       string
		agentMode  bool
		args       []string
		stdin      string
		wantStdout string         // exact match when non-empty and wantJSON is nil
		wantJSON   map[string]any // decoded single-JSON-value expectations
		wantYAML   bool           // decode stdout as YAML and check type field
		wantStderr []string       // substrings expected on stderr
		wantIDs    func(*fakeDeleteAPI) []string
	}{
		{
			name:       "human default is the byte-identical prose line",
			args:       []string{"delete", "R42", "--force"},
			wantStdout: "✔ Deleted route R42\n",
			wantIDs:    func(f *fakeDeleteAPI) []string { return f.deletedRoutes },
		},
		{
			name:      "agent mode emits exactly one JSON value",
			agentMode: true,
			args:      []string{"delete", "R42", "--force"},
			wantJSON: map[string]any{
				"type":           "gcx.mutation",
				"schema_version": "1",
				"action":         "deleted",
				"changed":        true,
			},
			wantIDs: func(f *fakeDeleteAPI) []string { return f.deletedRoutes },
		},
		{
			name: "explicit -o json wins outside agent mode",
			args: []string{"delete", "R42", "--force", "-o", "json"},
			wantJSON: map[string]any{
				"type":   "gcx.mutation",
				"action": "deleted",
			},
			wantIDs: func(f *fakeDeleteAPI) []string { return f.deletedRoutes },
		},
		{
			name:      "explicit -o yaml wins in agent mode",
			agentMode: true,
			args:      []string{"delete", "R42", "--force", "-o", "yaml"},
			wantYAML:  true,
			wantIDs:   func(f *fakeDeleteAPI) []string { return f.deletedRoutes },
		},
		{
			name:       "declined prompt keeps stdout empty and diagnostics on stderr",
			args:       []string{"delete", "R42"},
			stdin:      "n\n",
			wantStdout: "",
			wantStderr: []string{"Delete route R42? [y/N]", "Aborted."},
			wantIDs:    func(f *fakeDeleteAPI) []string { return nil },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)

			fake := &fakeDeleteAPI{}
			stdout, stderr, err := runNounCmd(t, func() *cobra.Command {
				return newRoutesCmd(&fakeLoader{client: fake})
			}, tc.stdin, tc.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			switch {
			case tc.wantJSON != nil:
				doc := decodeSingleJSONValue(t, stdout)
				for k, want := range tc.wantJSON {
					if got := doc[k]; got != want {
						t.Errorf("result[%q] = %v, want %v (doc=%v)", k, got, want, doc)
					}
				}
				target, _ := doc["target"].(map[string]any)
				if target["kind"] != "Route" || target["id"] != "R42" {
					t.Errorf("target = %v, want kind=Route id=R42", target)
				}
			case tc.wantYAML:
				var doc map[string]any
				if err := yaml.Unmarshal([]byte(stdout), &doc); err != nil {
					t.Fatalf("stdout is not YAML: %v\nstdout=%q", err, stdout)
				}
				if doc["type"] != "gcx.mutation" {
					t.Errorf("yaml type = %v, want gcx.mutation", doc["type"])
				}
			default:
				if stdout != tc.wantStdout {
					t.Errorf("stdout = %q, want %q", stdout, tc.wantStdout)
				}
			}

			for _, want := range tc.wantStderr {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr missing %q; stderr=%q", want, stderr)
				}
			}

			gotIDs := tc.wantIDs(fake)
			if tc.wantStdout == "" && tc.wantJSON == nil && !tc.wantYAML {
				if len(gotIDs) != 0 {
					t.Errorf("expected no deletion, got %v", gotIDs)
				}
			} else if len(gotIDs) != 1 || gotIDs[0] != "R42" {
				t.Errorf("expected delete of R42, got %v", gotIDs)
			}
		})
	}
}

// TestOnCallCRUDDeleteKindWiring proves the per-noun kind/label wiring is
// threaded through the shared builder (integrations as second representative).
func TestOnCallCRUDDeleteKindWiring(t *testing.T) {
	setAgentMode(t, false)

	fake := &fakeDeleteAPI{}
	stdout, _, err := runNounCmd(t, func() *cobra.Command {
		return newIntegrationsCmd(&fakeLoader{client: fake})
	}, "", "delete", "IABC", "--force")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "✔ Deleted integration IABC\n" {
		t.Errorf("stdout = %q, want %q", stdout, "✔ Deleted integration IABC\n")
	}

	setAgentMode(t, true)
	fake = &fakeDeleteAPI{}
	stdout, _, err = runNounCmd(t, func() *cobra.Command {
		return newIntegrationsCmd(&fakeLoader{client: fake})
	}, "", "delete", "IABC", "--force")
	if err != nil {
		t.Fatal(err)
	}
	doc := decodeSingleJSONValue(t, stdout)
	target, _ := doc["target"].(map[string]any)
	if target["kind"] != "Integration" || target["id"] != "IABC" {
		t.Errorf("target = %v, want kind=Integration id=IABC", target)
	}
	if len(fake.deletedIntegrations) != 1 || fake.deletedIntegrations[0] != "IABC" {
		t.Errorf("expected delete of IABC, got %v", fake.deletedIntegrations)
	}
}

// fakeFinalShiftsAPI returns an empty on-call window.
type fakeFinalShiftsAPI struct {
	OnCallAPI
}

func (f *fakeFinalShiftsAPI) ListFilterEvents(_ context.Context, _, _, _ string, _ int) (*FilterEventsResponse, error) {
	return &FilterEventsResponse{}, nil
}

// TestScheduleListFinalShiftsEmptyEncodesEmptyArray pins that an empty window
// encodes as [] (never null) for structured formats.
func TestScheduleListFinalShiftsEmptyEncodesEmptyArray(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
		args      []string
	}{
		{
			name: "explicit -o json",
			args: []string{"list-final-shifts", "SCHED1", "-o", "json"},
		},
		{
			name:      "agent mode default",
			agentMode: true,
			args:      []string{"list-final-shifts", "SCHED1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setAgentMode(t, tc.agentMode)

			stdout, _, err := runNounCmd(t, func() *cobra.Command {
				return newSchedulesCmd(&fakeLoader{client: &fakeFinalShiftsAPI{}})
			}, "", tc.args...)
			if err != nil {
				t.Fatal(err)
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
