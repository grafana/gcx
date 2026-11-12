//nolint:testpackage // white-box tests require access to unexported IRM command builders
package irm

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

func TestContainersGetYAMLMatchesJSON(t *testing.T) {
	setAgentMode(t, false)
	run := func(format string) string {
		stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newOpMemCmd(&fakeOpMemLoader{op: &fakeOpMem{}}) },
			"containers", "get", "INC-1", "-o", format)
		if err != nil {
			t.Fatalf("-o %s: %v", format, err)
		}
		return stdout
	}
	rawYAML := run("yaml")
	var fromYAML map[string]any
	if err := yaml.Unmarshal([]byte(rawYAML), &fromYAML); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	fromJSON := decodeSingleJSONValue(t, run("json"))
	if !reflect.DeepEqual(fromYAML, fromJSON) {
		t.Errorf("YAML document %v differs from JSON document %v", fromYAML, fromJSON)
	}
	if _, ok := fromYAML["observations"]; !ok {
		t.Errorf("observations is not a top-level YAML key:\n%s", rawYAML)
	}
	if !strings.HasPrefix(rawYAML, "type: gcx.irm.opmem.container\n") {
		t.Errorf("YAML must lead with the document type:\n%s", rawYAML)
	}
}

func TestContainersGetDefaultsToYAML(t *testing.T) {
	setAgentMode(t, false)
	stdout, _, err := runOpMemTree(t, func() *cobra.Command { return newOpMemCmd(&fakeOpMemLoader{op: &fakeOpMem{}}) }, "containers", "get", "INC-1")
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid([]byte(stdout)) {
		t.Errorf("default output is JSON; want YAML:\n%s", stdout)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(stdout), &doc); err != nil || doc["container"] != "INC-1" {
		t.Errorf("default output is not the YAML document (err=%v):\n%s", err, stdout)
	}
}
