package root_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/resources/adapter"
)

// readOnlyRegistrations are adapter-backed resource types without Create or
// Update support. ARCHITECTURE.md § Rules lets them omit Example, because an
// example is a template for a write.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var readOnlyRegistrations = map[string]bool{
	"kg.ext.grafana.app/v1alpha1, Kind=ModelRules":       true,
	"kg.ext.grafana.app/v1alpha1, Kind=Scope":            true,
	"oncall.ext.grafana.app/v1alpha1, Kind=Alert":        true,
	"oncall.ext.grafana.app/v1alpha1, Kind=AlertGroup":   true,
	"oncall.ext.grafana.app/v1alpha1, Kind=Organization": true,
	"oncall.ext.grafana.app/v1alpha1, Kind=SlackChannel": true,
	"oncall.ext.grafana.app/v1alpha1, Kind=Team":         true,
	"oncall.ext.grafana.app/v1alpha1, Kind=User":         true,
	"oncall.ext.grafana.app/v1alpha1, Kind=UserGroup":    true,
}

// writableRegistrationsMissingExample are writable resource types that shipped
// without an Example. This list only shrinks: add the Example, then remove the
// entry.
//
//nolint:gochecknoglobals // constant-like lookup table for test validation
var writableRegistrationsMissingExample = map[string]bool{
	"agento11y.ext.grafana.app/v1alpha1, Kind=Collection": true,
	"agento11y.ext.grafana.app/v1alpha1, Kind=EvalRule":   true,
	"agento11y.ext.grafana.app/v1alpha1, Kind=Evaluator":  true,
	"agento11y.ext.grafana.app/v1alpha1, Kind=HookRule":   true,
}

// TestRegistrations_SchemaAndExample fails when an adapter registration has no
// Schema, or a writable one has no Example. Without them `resources
// list-types` and `resources list-examples` degrade silently.
func TestRegistrations_SchemaAndExample(t *testing.T) {
	_ = buildRootCmd()

	regs := adapter.AllRegistrations()
	if len(regs) == 0 {
		t.Fatal("no adapter registrations found; providers did not register")
	}

	seen := map[string]bool{}
	for _, reg := range regs {
		gvk := reg.GVK.String()
		seen[gvk] = true
		if reg.Schema == nil || !usableJSON(reg.Schema()) {
			t.Errorf("registration %s has no usable Schema; every registration needs one that returns a JSON document (ARCHITECTURE.md § Rules)", gvk)
		}
		if len(reg.Example) > 0 {
			if !usableJSON(reg.Example) {
				t.Errorf("registration %s has an Example that is not a non-empty JSON document", gvk)
			}
			if writableRegistrationsMissingExample[gvk] {
				t.Errorf("registration %s now has an Example; remove it from writableRegistrationsMissingExample", gvk)
			}
			continue
		}
		if readOnlyRegistrations[gvk] || writableRegistrationsMissingExample[gvk] {
			continue
		}
		t.Errorf("registration %s has no Example; a writable registration needs one (ARCHITECTURE.md § Rules). If the resource is read-only, add it to readOnlyRegistrations", gvk)
	}

	reportStale(t, seen, readOnlyRegistrations, "readOnlyRegistrations entry %s no longer exists; remove it")
	reportStale(t, seen, writableRegistrationsMissingExample, "writableRegistrationsMissingExample entry %s no longer exists; remove it")
}

// usableJSON reports whether raw is valid JSON with content: not empty, not
// null, and not an empty object or array.
func usableJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if !json.Valid(trimmed) {
		return false
	}
	switch string(trimmed) {
	case "", "null", "{}", "[]":
		return false
	}
	return true
}
