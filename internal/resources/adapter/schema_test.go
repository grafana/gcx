package adapter_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type schemaTestWidget struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int    `json:"count,omitempty"`
}

// requireMap extracts a map[string]any from a parent map, failing the test if
// the key is missing or the value is not a map.
func requireMap(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key]
	require.True(t, ok, "key %q not found", key)
	result, ok := v.(map[string]any)
	require.True(t, ok, "key %q is %T, not map[string]any", key, v)
	return result
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

	var s map[string]any
	require.NoError(t, json.Unmarshal(raw, &s))

	t.Run("envelope has required top-level fields", func(t *testing.T) {
		assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", s["$schema"])
		assert.Equal(t, "https://grafana.com/schemas/Widget", s["$id"])
		assert.Equal(t, "object", s["type"])
		assert.Equal(t, []any{"apiVersion", "kind", "metadata", "spec"}, s["required"])
	})

	t.Run("apiVersion and kind are const-constrained", func(t *testing.T) {
		props := requireMap(t, s, "properties")

		apiVersion := requireMap(t, props, "apiVersion")
		assert.Equal(t, "string", apiVersion["type"])
		assert.Equal(t, "test.grafana.app/v1", apiVersion["const"])

		kind := requireMap(t, props, "kind")
		assert.Equal(t, "string", kind["type"])
		assert.Equal(t, "Widget", kind["const"])
	})

	t.Run("metadata has name and namespace", func(t *testing.T) {
		props := requireMap(t, s, "properties")
		metadata := requireMap(t, props, "metadata")
		assert.Equal(t, "object", metadata["type"])

		metaProps := requireMap(t, metadata, "properties")
		assert.Contains(t, metaProps, "name")
		assert.Contains(t, metaProps, "namespace")
	})

	t.Run("spec reflects Go struct fields", func(t *testing.T) {
		props := requireMap(t, s, "properties")
		spec := requireMap(t, props, "spec")
		assert.Equal(t, "object", spec["type"])

		specProps := requireMap(t, spec, "properties")
		assert.Contains(t, specProps, "name")
		assert.Contains(t, specProps, "color")
		assert.Contains(t, specProps, "count")

		nameField := requireMap(t, specProps, "name")
		assert.Equal(t, "string", nameField["type"])

		countField := requireMap(t, specProps, "count")
		assert.Equal(t, "integer", countField["type"])
	})
}
