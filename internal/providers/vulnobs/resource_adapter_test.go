package vulnobs_test

import (
	"encoding/json"
	"testing"

	"github.com/grafana/gcx/internal/providers/vulnobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceDescriptor_GVK(t *testing.T) {
	d := vulnobs.SourceDescriptor()
	gvk := d.GroupVersionKind()
	assert.Equal(t, "vulnobs.grafana.app", gvk.Group)
	assert.Equal(t, "v1alpha1", gvk.Version)
	assert.Equal(t, "Source", gvk.Kind)
	assert.Equal(t, "sources", d.Plural)
	assert.Equal(t, "source", d.Singular)
}

func TestSourceSchema_NonNilAndValidJSONSchema(t *testing.T) {
	raw := vulnobs.SourceSchema()
	require.NotEmpty(t, raw)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(raw, &parsed))

	assert.Equal(t, "https://json-schema.org/draft/2020-12/schema", parsed["$schema"])
	assert.Equal(t, "object", parsed["type"])

	props, ok := parsed["properties"].(map[string]any)
	require.True(t, ok)
	apiVersion, ok := props["apiVersion"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, vulnobs.APIVersion, apiVersion["const"])
	kind, ok := props["kind"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, vulnobs.SourceKind, kind["const"])

	// Spec captures the domain-relevant fields.
	spec, ok := props["spec"].(map[string]any)
	require.True(t, ok)
	specProps, ok := spec["properties"].(map[string]any)
	require.True(t, ok)
	for _, k := range []string{"name", "type", "origin", "visibility", "integration", "groups", "versions"} {
		assert.Contains(t, specProps, k, "spec should describe field %q", k)
	}
}

func TestSourceToResource_RoundTrip(t *testing.T) {
	src := vulnobs.Source{
		ID:         1064,
		Name:       "grafana/faro-web-sdk",
		Type:       "repository",
		Origin:     "github",
		Visibility: "public",
		Groups: []vulnobs.Group{
			{ID: 57, Name: "feO11y"},
		},
		Versions: []vulnobs.Version{
			{ID: 10354, Tag: "main"},
		},
	}

	res, err := vulnobs.SourceToResource(src, "stacks-123")
	require.NoError(t, err)
	require.NotNil(t, res)

	obj := res.Object.Object
	assert.Equal(t, vulnobs.APIVersion, obj["apiVersion"])
	assert.Equal(t, vulnobs.SourceKind, obj["kind"])

	meta, ok := obj["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "grafana--faro-web-sdk", meta["name"], "slash must be replaced for k8s metadata.name")
	assert.Equal(t, "stacks-123", meta["namespace"])

	spec, ok := obj["spec"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "grafana/faro-web-sdk", spec["name"], "spec preserves original owner/repo form")
}
