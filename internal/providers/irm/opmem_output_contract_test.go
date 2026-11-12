//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"testing"

	"github.com/spf13/cobra"
)

func runOpMemTree(t *testing.T, build func() *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	return runNounCmd(t, build, "", args...)
}

func TestOpMemUsageErrorEmitsNoResultDocument(t *testing.T) {
	setAgentMode(t, true)
	loader := &fakeOpMemLoader{op: &fakeOpMem{}}
	stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newOpMemCmd(loader) },
		"containers", "upsert-model", "INC-1", "-f", "draft.json",
		"--actor-kind", "robot", "--actor-id", "a")
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
	loader := &fakeOpMemLoader{op: &fakeOpMem{}}
	stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newOpMemCmd(loader) },
		"containers", "get", "INC-1")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	doc := decodeSingleJSONValue(t, stdout)
	if doc["type"] != "gcx.irm.opmem.container" {
		t.Errorf("wrong document type: %v", doc["type"])
	}
	if _, ok := doc["observations"].([]any); !ok {
		t.Errorf("observations must serialize as [], got %T", doc["observations"])
	}
}
