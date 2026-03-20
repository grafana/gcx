package k6_test

import (
	"testing"

	"github.com/grafana/grafanactl/internal/providers/k6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdapterRoundTrip(t *testing.T) {
	original := k6.Project{
		ID:               42,
		Name:             "my-project",
		IsDefault:        true,
		GrafanaFolderUID: "abc-123",
		Created:          "2026-01-01T00:00:00Z",
		Updated:          "2026-01-02T00:00:00Z",
	}

	// Project → Resource
	res, err := k6.ToResource(original, "stack-999")
	require.NoError(t, err)
	assert.Equal(t, "42", res.Raw.GetName())
	assert.Equal(t, "stack-999", res.Raw.GetNamespace())

	// Resource → Project
	roundTripped, err := k6.FromResource(res)
	require.NoError(t, err)
	assert.Equal(t, original.ID, roundTripped.ID)
	assert.Equal(t, original.Name, roundTripped.Name)
	assert.Equal(t, original.IsDefault, roundTripped.IsDefault)
	assert.Equal(t, original.GrafanaFolderUID, roundTripped.GrafanaFolderUID)
	assert.Equal(t, original.Created, roundTripped.Created)
	assert.Equal(t, original.Updated, roundTripped.Updated)
}

func TestToResource_SetsAPIVersionAndKind(t *testing.T) {
	p := k6.Project{ID: 1, Name: "test"}
	res, err := k6.ToResource(p, "stack-123")
	require.NoError(t, err)

	obj := res.Object.Object
	assert.Equal(t, k6.APIVersion, obj["apiVersion"])
	assert.Equal(t, k6.Kind, obj["kind"])
}

func TestToResource_StripsIDFromSpec(t *testing.T) {
	p := k6.Project{ID: 42, Name: "test"}
	res, err := k6.ToResource(p, "stack-123")
	require.NoError(t, err)

	spec, ok := res.Object.Object["spec"].(map[string]any)
	require.True(t, ok)
	_, hasID := spec["id"]
	assert.False(t, hasID, "spec should not contain the 'id' field (it belongs in metadata.name)")
}
