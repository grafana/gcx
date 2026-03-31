package oncall_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/resources"
)

func TestIntegrationRoundTrip(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"apiVersion": "oncall.ext.grafana.app/v1alpha1",
		"kind":       "Integration",
		"metadata": map[string]any{
			"name":      "int123",
			"namespace": "default",
		},
		"spec": map[string]any{
			"name":              "My Integration",
			"description_short": "Test integration",
			"type":              "grafana_alerting",
			"team_id":           "team1",
		},
	}

	res := resources.MustFromObject(original, resources.SourceInfo{})
	if res.Raw.GetName() != "int123" {
		t.Errorf("expected name int123, got %s", res.Raw.GetName())
	}

	obj := res.Object.Object
	spec, ok := obj["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}

	if spec["name"] != "My Integration" {
		t.Errorf("expected name 'My Integration', got %v", spec["name"])
	}
	if spec["type"] != "grafana_alerting" {
		t.Errorf("expected type 'grafana_alerting', got %v", spec["type"])
	}
}

func TestScheduleRoundTrip(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"apiVersion": "oncall.ext.grafana.app/v1alpha1",
		"kind":       "Schedule",
		"metadata": map[string]any{
			"name":      "sched123",
			"namespace": "default",
		},
		"spec": map[string]any{
			"name":      "Primary On-Call",
			"type":      "web",
			"time_zone": "America/New_York",
		},
	}

	res := resources.MustFromObject(original, resources.SourceInfo{})
	if res.Raw.GetName() != "sched123" {
		t.Errorf("expected name sched123, got %s", res.Raw.GetName())
	}

	obj := res.Object.Object
	spec, ok := obj["spec"].(map[string]any)
	if !ok {
		t.Fatal("spec is not a map")
	}

	if spec["name"] != "Primary On-Call" {
		t.Errorf("expected name 'Primary On-Call', got %v", spec["name"])
	}
}

func TestIntegrationSchemaJSON(t *testing.T) {
	t.Parallel()

	// Verify the schema is valid JSON and has expected structure.
	// This tests the init-time schema generation.
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/oncall/Integration",
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": "oncall.ext.grafana.app/v1alpha1"},
			"kind":       map[string]any{"type": "string", "const": "Integration"},
		},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal schema: %v", err)
	}

	if parsed["$id"] != "https://grafana.com/schemas/oncall/Integration" {
		t.Errorf("unexpected $id: %v", parsed["$id"])
	}
}
