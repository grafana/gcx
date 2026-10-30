//nolint:testpackage // The test checks registrations built by this package.
package irm

import (
	"encoding/json"
	"testing"
)

func TestOnCallSchemaContract(t *testing.T) {
	want := map[string]struct {
		fields   []string
		required string
	}{
		"Schedule": {fields: []string{"ical_url_overrides"}, required: "type"},
		"Shift":    {fields: []string{"type", "frequency"}, required: "type"},
		"Webhook":  {fields: []string{"trigger_type"}},
	}
	for _, registration := range buildOnCallRegistrations(&fakeLoader{}) {
		contract, ok := want[registration.GVK.Kind]
		if !ok {
			continue
		}
		var envelope struct {
			Properties struct {
				Spec struct {
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				} `json:"spec"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(registration.Schema(), &envelope); err != nil {
			t.Fatal(err)
		}
		for _, field := range contract.fields {
			if _, ok := envelope.Properties.Spec.Properties[field]; !ok {
				t.Errorf("%s schema omits %s", registration.GVK.Kind, field)
			}
		}
		if contract.required != "" {
			found := false
			for _, field := range envelope.Properties.Spec.Required {
				if field == contract.required {
					found = true
				}
			}
			if !found {
				t.Errorf("%s schema does not require %s", registration.GVK.Kind, contract.required)
			}
		}
		if registration.GVK.Kind == "Schedule" {
			var choice struct {
				Not struct {
					Enum []any `json:"enum"`
				} `json:"not"`
			}
			if err := json.Unmarshal(envelope.Properties.Spec.Properties["type"], &choice); err != nil {
				t.Fatal(err)
			}
			if len(choice.Not.Enum) != 2 || choice.Not.Enum[0] != nil || choice.Not.Enum[1] != "" {
				t.Errorf("Schedule type does not exclude null and empty string: %#v", choice.Not.Enum)
			}
			if err := validateScheduleType(0); err != nil {
				t.Errorf("Schedule type 0 is valid: %v", err)
			}
			for _, value := range []any{nil, ""} {
				if err := validateScheduleType(value); err == nil {
					t.Errorf("Schedule type %v must be rejected", value)
				}
			}
		}
		for field, count := range map[string]int{"frequency": 4, "trigger_type": 21} {
			raw, ok := envelope.Properties.Spec.Properties[field]
			if !ok {
				continue
			}
			var choice struct {
				OneOf []struct {
					Type string `json:"type"`
					Enum []any  `json:"enum"`
				} `json:"oneOf"`
			}
			if err := json.Unmarshal(raw, &choice); err != nil {
				t.Fatal(err)
			}
			if len(choice.OneOf) != 3 || choice.OneOf[0].Type != "integer" || choice.OneOf[1].Type != "string" || choice.OneOf[2].Type != "null" || len(choice.OneOf[0].Enum) != count {
				t.Errorf("%s %s choices = %#v", registration.GVK.Kind, field, choice)
			}
		}
	}
}
