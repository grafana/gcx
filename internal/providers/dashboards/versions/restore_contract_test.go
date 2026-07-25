package versions_test

// Contract tests for the `dashboards versions restore` migration to the
// pre-GA agent output contract:
//   - default human stdout stays byte-identical (empty — prompt, no-op note,
//     and success prose have always lived on stderr);
//   - agent mode emits exactly one JSON value: the gcx.dashboards.restore
//     document carrying the server-assigned new generation;
//   - explicit -o json/yaml overrides are honored.

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// restoreFake returns a fake client where the current dashboard is at
// generation 3 (resourceVersion rv-q) and generation 1 exists in history;
// a PUT returns generation 4.
func restoreFake() *fakeVersionsClient {
	return &fakeVersionsClient{
		historyItems: []unstructured.Unstructured{
			historyItem(1, "2024-01-01T00:00:00Z", "alice", "v1", map[string]any{"title": "v1"}),
			historyItem(3, "2024-01-03T00:00:00Z", "bob", "v3", map[string]any{"title": "v3"}),
		},
		currentItem: currentDashboard(3, "rv-q", map[string]any{"title": "v3"}),
		updatedItem: updatedDashboard(4),
	}
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

func TestVersionsRestore_HumanDefaultStdoutStaysEmpty(t *testing.T) {
	tests := []struct {
		name         string
		version      string
		wantInStderr string
	}{
		{name: "restore success", version: "1", wantInStderr: "restored to version 1 (new generation 4)"},
		{name: "no-op already at target", version: "3", wantInStderr: "already at version 3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetFlag(false)
			t.Cleanup(agent.ResetForTesting)

			stdout, stderr, err := runVersionsCmd(t, restoreFake(),
				[]string{"restore", "foo", tt.version, "--force"}, "")
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}

			// Byte-identical to the pre-codec behavior: nothing on stdout.
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, tt.wantInStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantInStderr)
			}
		})
	}
}

func TestVersionsRestore_StructuredResultDocument(t *testing.T) {
	tests := []struct {
		name          string
		agentMode     bool
		extraArgs     []string
		version       string
		yamlOutput    bool
		wantChanged   bool
		wantRestored  float64
		wantNewGen    float64
		wantPutIssued bool
	}{
		{
			name:          "agent mode restore",
			agentMode:     true,
			version:       "1",
			wantChanged:   true,
			wantRestored:  1,
			wantNewGen:    4,
			wantPutIssued: true,
		},
		{
			name:         "agent mode no-op",
			agentMode:    true,
			version:      "3",
			wantChanged:  false,
			wantRestored: 3,
			wantNewGen:   3,
		},
		{
			name:          "explicit -o json human mode",
			extraArgs:     []string{"-o", "json"},
			version:       "1",
			wantChanged:   true,
			wantRestored:  1,
			wantNewGen:    4,
			wantPutIssued: true,
		},
		{
			name:          "explicit -o yaml agent mode",
			agentMode:     true,
			extraArgs:     []string{"-o", "yaml"},
			version:       "1",
			yamlOutput:    true,
			wantChanged:   true,
			wantRestored:  1,
			wantNewGen:    4,
			wantPutIssued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent.SetFlag(tt.agentMode)
			t.Cleanup(agent.ResetForTesting)

			fc := restoreFake()
			args := append([]string{"restore", "foo", tt.version, "--force"}, tt.extraArgs...)
			stdout, _, err := runVersionsCmd(t, fc, args, "")
			if err != nil {
				t.Fatalf("Execute() = %v", err)
			}

			var doc map[string]any
			if tt.yamlOutput {
				if yamlErr := yaml.Unmarshal([]byte(stdout), &doc); yamlErr != nil {
					t.Fatalf("stdout is not valid YAML: %v\n%s", yamlErr, stdout)
				}
			} else {
				doc = decodeSingleJSONDoc(t, stdout)
			}

			if doc["type"] != "gcx.dashboards.restore" {
				t.Errorf("type = %v, want gcx.dashboards.restore", doc["type"])
			}
			if doc["schema_version"] != "1" {
				t.Errorf("schema_version = %v, want 1", doc["schema_version"])
			}
			if doc["action"] != "restored" {
				t.Errorf("action = %v, want restored", doc["action"])
			}
			if doc["changed"] != tt.wantChanged {
				t.Errorf("changed = %v, want %v", doc["changed"], tt.wantChanged)
			}
			if got, ok := doc["restored_version"].(float64); !ok || got != tt.wantRestored {
				t.Errorf("restored_version = %v, want %v", doc["restored_version"], tt.wantRestored)
			}
			if got, ok := doc["new_generation"].(float64); !ok || got != tt.wantNewGen {
				t.Errorf("new_generation = %v, want %v", doc["new_generation"], tt.wantNewGen)
			}

			target, ok := doc["target"].(map[string]any)
			if !ok {
				t.Fatalf("target = %v, want object", doc["target"])
			}
			if target["name"] != "foo" {
				t.Errorf("target.name = %v, want foo", target["name"])
			}
			if target["kind"] != "Dashboard" {
				t.Errorf("target.kind = %v, want Dashboard", target["kind"])
			}

			if fc.updateCalled != tt.wantPutIssued {
				t.Errorf("update called = %v, want %v", fc.updateCalled, tt.wantPutIssued)
			}
		})
	}
}

func TestVersionsRestore_DeclinedPromptEmitsNoDocument(t *testing.T) {
	agent.SetFlag(false)
	t.Cleanup(agent.ResetForTesting)
	t.Setenv("GCX_AUTO_APPROVE", "false")

	fc := restoreFake()
	stdout, stderr, err := runVersionsCmd(t, fc, []string{"restore", "foo", "1"}, "n\n")
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}

	if stdout != "" {
		t.Errorf("stdout = %q, want empty on declined prompt", stdout)
	}
	if !strings.Contains(stderr, "Aborted.") {
		t.Errorf("stderr = %q, want it to contain Aborted.", stderr)
	}
	if fc.updateCalled {
		t.Error("no PUT must be issued when the prompt is declined")
	}
}
