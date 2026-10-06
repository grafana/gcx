package root_test

import "testing"

func TestProfilesAdaptiveAgentError(t *testing.T) {
	stdout, code := runGcx(t, "profiles", "adaptive")
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	value, ok := assertOneJSONValue(t, stdout).(map[string]any)
	if !ok {
		t.Fatalf("error document is not an object: %T", value)
	}
	if value["type"] != "gcx.error" {
		t.Fatalf("type = %v, want gcx.error", value["type"])
	}
	failure, ok := value["error"].(map[string]any)
	if !ok {
		t.Fatal("error object is missing")
	}
	if failure["summary"] != "Adaptive Profiles management is not supported by gcx" {
		t.Fatalf("summary = %v", failure["summary"])
	}
	if failure["docsLink"] != "https://grafana.com/docs/grafana-cloud/observe-and-act/adaptive-telemetry/adaptive-profiles/" {
		t.Fatalf("docsLink = %v", failure["docsLink"])
	}
}
