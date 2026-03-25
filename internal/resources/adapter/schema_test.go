package adapter_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type schemaTestWidget struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int    `json:"count,omitempty"`
}

func TestSchemaFromType(t *testing.T) {
	desc := resources.Descriptor{
		GroupVersion: schema.GroupVersion{Group: "test.grafana.app", Version: "v1"},
		Kind:         "Widget",
		Singular:     "widget",
		Plural:       "widgets",
	}

	raw := adapter.SchemaFromType[schemaTestWidget](desc)
	require.NotNil(t, raw)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))

	t.Run("envelope has required top-level fields", func(t *testing.T) {
		assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", schema["$schema"])
		assert.Equal(t, "https://grafana.com/schemas/Widget", schema["$id"])
		assert.Equal(t, "object", schema["type"])
		assert.Equal(t, []any{"apiVersion", "kind", "metadata", "spec"}, schema["required"])
	})

	t.Run("apiVersion and kind are const-constrained", func(t *testing.T) {
		props := schema["properties"].(map[string]any)

		apiVersion := props["apiVersion"].(map[string]any)
		assert.Equal(t, "string", apiVersion["type"])
		assert.Equal(t, "test.grafana.app/v1", apiVersion["const"])

		kind := props["kind"].(map[string]any)
		assert.Equal(t, "string", kind["type"])
		assert.Equal(t, "Widget", kind["const"])
	})

	t.Run("metadata has name and namespace", func(t *testing.T) {
		props := schema["properties"].(map[string]any)
		metadata := props["metadata"].(map[string]any)
		assert.Equal(t, "object", metadata["type"])

		metaProps := metadata["properties"].(map[string]any)
		assert.Contains(t, metaProps, "name")
		assert.Contains(t, metaProps, "namespace")
	})

	t.Run("spec reflects Go struct fields", func(t *testing.T) {
		props := schema["properties"].(map[string]any)
		spec := props["spec"].(map[string]any)
		assert.Equal(t, "object", spec["type"])

		specProps := spec["properties"].(map[string]any)
		assert.Contains(t, specProps, "name")
		assert.Contains(t, specProps, "color")
		assert.Contains(t, specProps, "count")

		nameField := specProps["name"].(map[string]any)
		assert.Equal(t, "string", nameField["type"])

		countField := specProps["count"].(map[string]any)
		assert.Equal(t, "integer", countField["type"])
	})
}
