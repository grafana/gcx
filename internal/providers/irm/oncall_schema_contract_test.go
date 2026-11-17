//nolint:testpackage // The test checks registrations built by this package.
package irm

import (
	"encoding/json"
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestOnCallSchemaContract(t *testing.T) {
	want := map[string]struct {
		fields        []string
		required      string
		numericFields map[string]bool
		spec          any
	}{
		"Schedule": {spec: Schedule{}, fields: []string{"ical_url_overrides"}, required: "type", numericFields: map[string]bool{"type": false}},
		"Shift":    {spec: Shift{}, fields: []string{"type", "frequency"}, required: "type", numericFields: map[string]bool{"type": false, "frequency": true}},
		"Webhook":  {spec: Webhook{}, fields: []string{"trigger_type"}, numericFields: map[string]bool{"trigger_type": true}},
	}
	seen := make(map[string]bool)
	for _, registration := range buildOnCallRegistrations(&fakeLoader{}) {
		contract, ok := want[registration.GVK.Kind]
		if !ok {
			continue
		}
		seen[registration.GVK.Kind] = true
		reflected := (&jsonschema.Reflector{DoNotReference: true}).Reflect(contract.spec)
		reflected.Version = ""
		reflected.ID = ""
		raw, err := json.Marshal(reflected)
		require.NoError(t, err)
		var envelope struct {
			Properties struct {
				Spec json.RawMessage `json:"spec"`
			} `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(registration.Schema(), &envelope))
		require.JSONEq(t, string(raw), string(envelope.Properties.Spec), "%s registration must use its type schema", registration.GVK.Kind)
		var spec struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		require.NoError(t, json.Unmarshal(raw, &spec))
		for _, field := range contract.fields {
			if _, ok := spec.Properties[field]; !ok {
				t.Errorf("%s schema omits %s", registration.GVK.Kind, field)
			}
		}
		if contract.required != "" {
			found := false
			for _, field := range spec.Required {
				if field == contract.required {
					found = true
				}
			}
			if !found {
				t.Errorf("%s schema does not require %s", registration.GVK.Kind, contract.required)
			}
		}
		for field, optional := range contract.numericFields {
			raw := spec.Properties[field]
			var choice struct {
				Description string `json:"description"`
				OneOf       []struct {
					Type    string   `json:"type"`
					Pattern string   `json:"pattern"`
					Minimum *float64 `json:"minimum"`
				} `json:"oneOf"`
			}
			if err := json.Unmarshal(raw, &choice); err != nil {
				t.Fatal(err)
			}
			if choice.Description == "" {
				t.Errorf("%s %s has no numeric field description", registration.GVK.Kind, field)
			}
			wantBranches := 2
			wantPattern := `^(0|[1-9][0-9]*)$`
			if optional {
				wantBranches = 3
			}
			if len(choice.OneOf) != wantBranches || choice.OneOf[0].Type != "integer" || choice.OneOf[0].Minimum == nil || *choice.OneOf[0].Minimum != 0 || choice.OneOf[1].Type != "string" || choice.OneOf[1].Pattern != wantPattern {
				t.Errorf("%s %s numeric schema = %#v", registration.GVK.Kind, field, choice)
			}
			if optional && len(choice.OneOf) == 3 && choice.OneOf[2].Type != "null" {
				t.Errorf("%s %s must allow null", registration.GVK.Kind, field)
			}
		}
		if registration.GVK.Kind == "Webhook" {
			var example struct {
				Spec struct {
					TriggerType any `json:"trigger_type"`
				} `json:"spec"`
			}
			if err := json.Unmarshal(registration.Example, &example); err != nil {
				t.Fatal(err)
			}
			if example.Spec.TriggerType != float64(1) {
				t.Errorf("Webhook example trigger_type = %#v, want numeric 1", example.Spec.TriggerType)
			}
		}
	}
	for kind := range want {
		if !seen[kind] {
			t.Errorf("registration for %s is missing", kind)
		}
	}
}
