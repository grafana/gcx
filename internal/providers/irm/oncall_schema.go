package irm

import "github.com/invopop/jsonschema"

// JSONSchemaExtend defines the Schedule numeric type during schema reflection.
func (Schedule) JSONSchemaExtend(schema *jsonschema.Schema) {
	schema.Properties.Set("type", onCallNumberSchema(false,
		"Required plugin schedule type. Use the numeric value from an existing schedule."))
}

// JSONSchemaExtend defines the Shift numeric fields during schema reflection.
func (Shift) JSONSchemaExtend(schema *jsonschema.Schema) {
	schema.Properties.Set("type", onCallNumberSchema(false,
		"Internal shift type. The observed create endpoint advertises 2 rolling users and 3 override. The server checks supported values."))
	schema.Properties.Set("frequency", onCallNumberSchema(true,
		"Observed recurrence values: 0 daily, 1 weekly, 2 monthly, 3 hourly. The server checks supported values."))
}

// JSONSchemaExtend defines the Webhook trigger during schema reflection.
func (Webhook) JSONSchemaExtend(schema *jsonschema.Schema) {
	schema.Properties.Set("trigger_type", onCallNumberSchema(true,
		"OnCall webhook trigger value. Run webhooks list-triggers to see current names and values."))
}

func onCallNumberSchema(nullable bool, description string) *jsonschema.Schema {
	choices := []*jsonschema.Schema{
		{Type: "integer", Minimum: "0"},
		{Type: "string", Pattern: `^(0|[1-9][0-9]*)$`},
	}
	if nullable {
		choices = append(choices, &jsonschema.Schema{Type: "null"})
	}
	return &jsonschema.Schema{OneOf: choices, Description: description}
}
