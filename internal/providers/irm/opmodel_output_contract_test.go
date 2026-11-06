//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"encoding/json"
	"errors"
	"testing"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
)

func runOpModelTree(t *testing.T, build func() *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	return runNounCmd(t, build, "", args...)
}

func TestOpModelMutationOutputContract(t *testing.T) {
	tests := []struct {
		name      string
		agentMode bool
	}{
		{"agent mode emits one JSON value", true},
		{"tty mode emits one text line", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAgentMode(t, tt.agentMode)
			loader := &fakeOpModelLoader{op: &fakeOpModel{}}
			stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newOpModelCmd(loader) },
				"containers", "create-claim", "INC-1", "--type", "impact", "--text", "t",
				"--actor-kind", "agent", "--actor-id", "a")
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if tt.agentMode {
				doc := decodeSingleJSONValue(t, stdout)
				if doc["type"] != "gcx.irm.opmodel.mutation" {
					t.Errorf("agent-mode stdout must be the mutation envelope, got %v", doc)
				}
				return
			}
			if stdout != "create-claim claim clm-1: ok (revision 101)\n" {
				t.Errorf("tty text line wrong: %q", stdout)
			}
		})
	}
}

func TestOpModelUsageErrorEmitsNoResultDocument(t *testing.T) {
	setAgentMode(t, true)
	loader := &fakeOpModelLoader{op: &fakeOpModel{}}
	stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newOpModelCmd(loader) },
		"containers", "create-claim", "INC-1", "--type", "conclusion", "--text", "t",
		"--actor-kind", "agent", "--actor-id", "a")
	requireUsageExit(t, err)
	if stdout != "" {
		t.Errorf("stdout must stay empty on a usage error, got %q", stdout)
	}
	if loader.loads != 0 {
		t.Error("usage error must reject before any client load")
	}
}

func TestContainersGetAgentModeOutputContract(t *testing.T) {
	setAgentMode(t, true)
	loader := &fakeOpModelLoader{op: &fakeOpModel{}}
	stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newOpModelCmd(loader) },
		"containers", "get", "INC-1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	doc := decodeSingleJSONValue(t, stdout)
	if doc["type"] != "gcx.irm.opmodel.container" {
		t.Errorf("wrong document type: %v", doc["type"])
	}
	if _, ok := doc["observations"].([]any); !ok {
		t.Errorf("observations must serialize as [], got %T", doc["observations"])
	}
}

func TestSharesPublishAgentModeOutputContract(t *testing.T) {
	setAgentMode(t, true)
	pkg := validBasePackage()
	path := writeSharePackage(t, pkg)
	loader := &fakeOpModelLoader{op: &fakeOpModel{failAt: 2}}
	stdout, _, err := runOpModelTree(t, func() *cobra.Command { return newSharesCmd(loader) },
		"publish", "-f", path)

	var emitted *fail.EmittedError
	if !errors.As(err, &emitted) || emitted.Code != fail.ExitPartialFailure {
		t.Fatalf("expected EmittedError with exit 4, got %v", err)
	}
	doc := decodeSingleJSONValue(t, stdout)
	if doc["type"] != "gcx.irm.share_receipt" {
		t.Errorf("wrong document type: %v", doc["type"])
	}
	var receipt shareReceipt
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatalf("receipt decode: %v", err)
	}
	if receipt.Summary.Failed != 1 {
		t.Errorf("receipt summary wrong: %+v", receipt.Summary)
	}
}
