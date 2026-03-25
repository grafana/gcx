package adapter

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafanactl/internal/resources"
	"github.com/invopop/jsonschema"
)

// SchemaFromType generates a JSON Schema for a resource type T, wrapped in a
// Kubernetes-style envelope (apiVersion, kind, metadata, spec). The spec schema
// is derived by reflecting on T's struct tags.
//
// This is a convenience helper for providers that don't hand-write schemas.
// Providers that need richer schema annotations (e.g., enums, descriptions)
// should hand-write their schemas and pass them directly.
func SchemaFromType[T any](desc resources.Descriptor) json.RawMessage {
	// Generate spec schema from the Go type.
	r := &jsonschema.Reflector{
		DoNotReference: true,
	}
	specSchema := r.Reflect(new(T))

	// Remove the top-level $schema and $id from the spec — they belong on the
	// envelope, not on the nested spec object.
	specSchema.Version = ""
	specSchema.ID = ""

	specBytes, err := json.Marshal(specSchema)
	if err != nil {
		panic(fmt.Sprintf("adapter.SchemaFromType: marshal spec schema for %s: %v", desc.Kind, err))
	}

	// Re-parse as map to embed in the envelope.
	var specMap map[string]any
	if err := json.Unmarshal(specBytes, &specMap); err != nil {
		panic(fmt.Sprintf("adapter.SchemaFromType: unmarshal spec schema for %s: %v", desc.Kind, err))
	}

	envelope := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/" + desc.Kind,
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": desc.GroupVersion.String()},
			"kind":       map[string]any{"type": "string", "const": desc.Kind},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": specMap,
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}

	b, err := json.Marshal(envelope)
	if err != nil {
		panic(fmt.Sprintf("adapter.SchemaFromType: marshal envelope for %s: %v", desc.Kind, err))
	}
	return b
}
