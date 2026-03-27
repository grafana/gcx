package agent_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
)

func TestKnownResourcesNotEmpty(t *testing.T) {
	if len(agent.KnownResources) == 0 {
		t.Fatal("KnownResources is empty")
	}
}

func TestKnownResourcesHaveRequiredFields(t *testing.T) {
	for _, r := range agent.KnownResources {
		if r.Kind == "" {
			t.Error("KnownResource has empty Kind")
		}
		if r.Group == "" {
			t.Errorf("KnownResource %q has empty Group", r.Kind)
		}
		if r.Version == "" {
			t.Errorf("KnownResource %q has empty Version", r.Kind)
		}
		if len(r.Aliases) == 0 {
			t.Errorf("KnownResource %q has no aliases", r.Kind)
		}
		if len(r.Operations) == 0 {
			t.Errorf("KnownResource %q has no operations", r.Kind)
		}
		for op, hint := range r.Operations {
			if hint.TokenCost == "" {
				t.Errorf("KnownResource %q operation %q has empty TokenCost", r.Kind, op)
			}
			if hint.LLMHint == "" {
				t.Errorf("KnownResource %q operation %q has empty LLMHint", r.Kind, op)
			}
		}
	}
}

func TestKnownResourcesNoDuplicateGVK(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range agent.KnownResources {
		key := r.Kind + "." + r.Group
		if seen[key] {
			t.Errorf("duplicate KnownResource: %s", key)
		}
		seen[key] = true
	}
}

func TestBuilderReadOnlyDefaults(t *testing.T) {
	// Access the builder via KnownResources — find a read-only resource
	var found *agent.KnownResource
	for i := range agent.KnownResources {
		if agent.KnownResources[i].Kind == "Playlist" {
			found = &agent.KnownResources[i]
			break
		}
	}
	if found == nil {
		t.Fatal("Playlist not found in KnownResources")
	}

	if len(found.Operations) != 1 {
		t.Errorf("expected 1 operation for read-only resource, got %d", len(found.Operations))
	}
	if _, ok := found.Operations["get"]; !ok {
		t.Error("read-only resource missing 'get' operation")
	}
}

func TestBuilderCRUDDefaults(t *testing.T) {
	var found *agent.KnownResource
	for i := range agent.KnownResources {
		if agent.KnownResources[i].Kind == "Folder" {
			found = &agent.KnownResources[i]
			break
		}
	}
	if found == nil {
		t.Fatal("Folder not found in KnownResources")
	}

	expectedOps := []string{"get", "push", "pull", "delete"}
	if len(found.Operations) != len(expectedOps) {
		t.Fatalf("expected %d operations, got %d", len(expectedOps), len(found.Operations))
	}
	for _, op := range expectedOps {
		if _, ok := found.Operations[op]; !ok {
			t.Errorf("CRUD resource missing %q operation", op)
		}
	}

	// Folder should use default "medium" for get (no override)
	if found.Operations["get"].TokenCost != "medium" {
		t.Errorf("Folder get token_cost = %q, want medium", found.Operations["get"].TokenCost)
	}
}

func TestBuilderCostOverride(t *testing.T) {
	var found *agent.KnownResource
	for i := range agent.KnownResources {
		if agent.KnownResources[i].Kind == "Dashboard" {
			found = &agent.KnownResources[i]
			break
		}
	}
	if found == nil {
		t.Fatal("Dashboard not found in KnownResources")
	}

	// Dashboard overrides get and pull to "large"
	if found.Operations["get"].TokenCost != "large" {
		t.Errorf("Dashboard get token_cost = %q, want large", found.Operations["get"].TokenCost)
	}
	if found.Operations["pull"].TokenCost != "large" {
		t.Errorf("Dashboard pull token_cost = %q, want large", found.Operations["pull"].TokenCost)
	}
	// push should remain default "small"
	if found.Operations["push"].TokenCost != "small" {
		t.Errorf("Dashboard push token_cost = %q, want small", found.Operations["push"].TokenCost)
	}
}

func TestBuilderHintsUsePlural(t *testing.T) {
	var found *agent.KnownResource
	for i := range agent.KnownResources {
		if agent.KnownResources[i].Kind == "Folder" {
			found = &agent.KnownResources[i]
			break
		}
	}
	if found == nil {
		t.Fatal("Folder not found")
	}

	if hint := found.Operations["get"].LLMHint; hint != "gcx resources get folders -o json" {
		t.Errorf("get hint = %q", hint)
	}
	if hint := found.Operations["push"].LLMHint; hint != "gcx resources push -p ./folders" {
		t.Errorf("push hint = %q", hint)
	}
	if hint := found.Operations["pull"].LLMHint; hint != "gcx resources pull folders -p ./folders" {
		t.Errorf("pull hint = %q", hint)
	}
	if hint := found.Operations["delete"].LLMHint; hint != "gcx resources delete folders/NAME --dry-run" {
		t.Errorf("delete hint = %q", hint)
	}
}
