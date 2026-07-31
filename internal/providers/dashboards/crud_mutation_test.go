package dashboards_test

// Contract tests for the dashboards create/update/delete migration to the
// pre-GA agent output contract:
//   - default human stdout is byte-identical to the pre-codec prose receipt;
//   - agent mode emits exactly one JSON value (a gcx.mutation document);
//   - explicit -o json/yaml overrides are honored in both modes;
//   - the delete confirmation prompt and "Aborted." note live on stderr,
//     keeping stdout clean for the result document.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/dashboards"
	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// fakeMutationClient implements dashboards.DashboardMutationClient without
// real K8s connectivity.
type fakeMutationClient struct {
	createErr error
	updateErr error
	deleteErr error

	createCalled bool
	updateCalled bool
	deleteCalled bool
	deletedName  string
}

func (f *fakeMutationClient) Create(
	_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.CreateOptions,
) (*unstructured.Unstructured, error) {
	f.createCalled = true
	if f.createErr != nil {
		return nil, f.createErr
	}
	created := obj.DeepCopy()
	created.SetUID("uid-created-123")
	created.SetNamespace("default")
	return created, nil
}

func (f *fakeMutationClient) Update(
	_ context.Context, _ resources.Descriptor, obj *unstructured.Unstructured, _ metav1.UpdateOptions,
) (*unstructured.Unstructured, error) {
	f.updateCalled = true
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	updated := obj.DeepCopy()
	updated.SetUID("uid-updated-456")
	updated.SetNamespace("default")
	return updated, nil
}

func (f *fakeMutationClient) Delete(
	_ context.Context, _ resources.Descriptor, name string, _ metav1.DeleteOptions,
) error {
	f.deleteCalled = true
	f.deletedName = name
	return f.deleteErr
}

// mutationTestDesc returns a minimal Descriptor for dashboard resources.
func mutationTestDesc() resources.Descriptor {
	return resources.Descriptor{
		Kind:     "Dashboard",
		Singular: "dashboard",
		Plural:   "dashboards",
	}
}

// writeTestManifest writes a valid JSON dashboard manifest and returns its path.
func writeTestManifest(t *testing.T, name string) string {
	t.Helper()
	manifest := `{"apiVersion":"dashboard.grafana.app/v1","kind":"Dashboard","metadata":{"name":"` + name + `"},"spec":{"title":"Test"}}`
	path := filepath.Join(t.TempDir(), "dashboard.json")
	if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// runMutationCmd executes the given command with args and returns stdout,
// stderr, and the error from Execute.
func runMutationCmd(t *testing.T, cmd *cobra.Command, args []string, stdin string) (string, string, error) {
	t.Helper()

	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)

	err := cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// buildCommand constructs one of the three mutation commands with the fake
// client wired in. Agent mode must be pinned BEFORE calling this: the codec
// default is resolved when flags are bound.
func buildCommand(verb string, fc *fakeMutationClient) *cobra.Command {
	desc := mutationTestDesc()
	switch verb {
	case "create":
		return dashboards.NewTestCreateCommand(fc, desc)
	case "update":
		return dashboards.NewTestUpdateCommand(fc, desc)
	case "delete":
		return dashboards.NewTestDeleteCommand(fc, desc)
	}
	panic("unknown verb " + verb)
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

func TestMutationCommands_HumanDefaultByteIdentical(t *testing.T) {
	tests := []struct {
		verb       string
		args       func(manifest string) []string
		wantStdout string
	}{
		{
			verb:       "create",
			args:       func(m string) []string { return []string{"-f", m} },
			wantStdout: "✔ dashboard \"my-dash\" created\n",
		},
		{
			verb:       "update",
			args:       func(m string) []string { return []string{"my-dash", "-f", m} },
			wantStdout: "✔ dashboard \"my-dash\" updated\n",
		},
		{
			verb:       "delete",
			args:       func(string) []string { return []string{"my-dash", "--force"} },
			wantStdout: "✔ dashboard \"my-dash\" deleted\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			agent.SetFlag(false)
			t.Cleanup(agent.ResetForTesting)
			oldNoColor := color.NoColor
			color.NoColor = true
			t.Cleanup(func() { color.NoColor = oldNoColor })

			fc := &fakeMutationClient{}
			cmd := buildCommand(tt.verb, fc)

			stdout, _, err := runMutationCmd(t, cmd, tt.args(writeTestManifest(t, "my-dash")), "")
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}

			// Byte-identical to the pre-codec cmdio.Success line.
			if stdout != tt.wantStdout {
				t.Fatalf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
		})
	}
}

func TestMutationCommands_AgentModeSingleJSONDoc(t *testing.T) {
	tests := []struct {
		verb        string
		args        func(manifest string) []string
		wantAction  string
		wantChanged bool // whether a "changed": true field must be present
	}{
		{
			verb:        "create",
			args:        func(m string) []string { return []string{"-f", m} },
			wantAction:  "created",
			wantChanged: true,
		},
		{
			verb:       "update",
			args:       func(m string) []string { return []string{"my-dash", "-f", m} },
			wantAction: "updated",
		},
		{
			verb:        "delete",
			args:        func(string) []string { return []string{"my-dash", "--force"} },
			wantAction:  "deleted",
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			agent.SetFlag(true)
			t.Cleanup(agent.ResetForTesting)

			fc := &fakeMutationClient{}
			cmd := buildCommand(tt.verb, fc)

			stdout, _, err := runMutationCmd(t, cmd, tt.args(writeTestManifest(t, "my-dash")), "")
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}

			doc := decodeSingleJSONDoc(t, stdout)
			if doc["type"] != "gcx.mutation" {
				t.Errorf("type = %v, want gcx.mutation", doc["type"])
			}
			if doc["schema_version"] != "1" {
				t.Errorf("schema_version = %v, want 1", doc["schema_version"])
			}
			if doc["action"] != tt.wantAction {
				t.Errorf("action = %v, want %s", doc["action"], tt.wantAction)
			}

			target, ok := doc["target"].(map[string]any)
			if !ok {
				t.Fatalf("target = %v, want object", doc["target"])
			}
			if target["name"] != "my-dash" {
				t.Errorf("target.name = %v, want my-dash", target["name"])
			}
			if target["kind"] != "Dashboard" {
				t.Errorf("target.kind = %v, want Dashboard", target["kind"])
			}

			changed, hasChanged := doc["changed"]
			if tt.wantChanged {
				if changed != true {
					t.Errorf("changed = %v, want true", changed)
				}
			} else if hasChanged {
				// Update cannot tell a no-op from a real change; the field
				// must be omitted rather than guessed.
				t.Errorf("changed present (= %v), want omitted", changed)
			}
		})
	}
}

func TestMutationCommands_ExplicitFormatOverride(t *testing.T) {
	tests := []struct {
		name      string
		verb      string
		agentMode bool
		format    string
	}{
		// -o json in human mode wins over the text default.
		{name: "create -o json human", verb: "create", format: "json"},
		{name: "delete -o json human", verb: "delete", format: "json"},
		// -o yaml in agent mode wins over the agents default.
		{name: "update -o yaml agent", verb: "update", agentMode: true, format: "yaml"},
		{name: "delete -o yaml agent", verb: "delete", agentMode: true, format: "yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetFlag(tt.agentMode)
			t.Cleanup(agent.ResetForTesting)

			fc := &fakeMutationClient{}
			cmd := buildCommand(tt.verb, fc)

			manifest := writeTestManifest(t, "my-dash")
			args := make([]string, 0, 4)
			switch tt.verb {
			case "create":
				args = append(args, "-f", manifest)
			case "update":
				args = append(args, "my-dash", "-f", manifest)
			case "delete":
				args = append(args, "my-dash", "--force")
			}
			args = append(args, "-o", tt.format)

			stdout, _, err := runMutationCmd(t, cmd, args, "")
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}

			var doc map[string]any
			switch tt.format {
			case "json":
				doc = decodeSingleJSONDoc(t, stdout)
			case "yaml":
				if err := yaml.Unmarshal([]byte(stdout), &doc); err != nil {
					t.Fatalf("stdout is not valid YAML: %v\n%s", err, stdout)
				}
			}

			if doc["type"] != "gcx.mutation" {
				t.Errorf("type = %v, want gcx.mutation\n%s", doc["type"], stdout)
			}
			if doc["schema_version"] != "1" {
				t.Errorf("schema_version = %v, want 1", doc["schema_version"])
			}
		})
	}
}

func TestDeleteCommand_PromptContract(t *testing.T) {
	tests := []struct {
		name         string
		stdin        string
		wantDeleted  bool
		wantStdout   string // exact stdout
		wantInStderr []string
	}{
		{
			name:         "decline leaves stdout empty",
			stdin:        "n\n",
			wantDeleted:  false,
			wantStdout:   "",
			wantInStderr: []string{`Delete dashboard "my-dash"?`, "Aborted."},
		},
		{
			name:         "confirm prints receipt on stdout only",
			stdin:        "y\n",
			wantDeleted:  true,
			wantStdout:   "✔ dashboard \"my-dash\" deleted\n",
			wantInStderr: []string{`Delete dashboard "my-dash"?`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetFlag(false)
			t.Cleanup(agent.ResetForTesting)
			oldNoColor := color.NoColor
			color.NoColor = true
			t.Cleanup(func() { color.NoColor = oldNoColor })
			// Pin auto-approve off so the prompt actually fires.
			t.Setenv("GCX_AUTO_APPROVE", "false")

			fc := &fakeMutationClient{}
			cmd := buildCommand("delete", fc)

			stdout, stderr, err := runMutationCmd(t, cmd, []string{"my-dash"}, tt.stdin)
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}

			if fc.deleteCalled != tt.wantDeleted {
				t.Errorf("delete called = %v, want %v", fc.deleteCalled, tt.wantDeleted)
			}
			if stdout != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			for _, want := range tt.wantInStderr {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr = %q, want it to contain %q", stderr, want)
				}
			}
		})
	}
}

func TestDeleteCommand_AgentModeRequiresForce(t *testing.T) {
	agent.SetFlag(true)
	t.Cleanup(agent.ResetForTesting)
	t.Setenv("GCX_AUTO_APPROVE", "false")

	fc := &fakeMutationClient{}
	cmd := buildCommand("delete", fc)

	stdout, _, err := runMutationCmd(t, cmd, []string{"my-dash"}, "")
	if err == nil {
		t.Fatal("Execute() = nil, want agent-mode-requires-force error")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %v, want it to mention --force", err)
	}
	if fc.deleteCalled {
		t.Error("delete must not be called without --force in agent mode")
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (error rendering is owned by reportError)", stdout)
	}
}
