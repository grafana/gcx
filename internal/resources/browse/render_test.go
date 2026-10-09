package browse_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources/browse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRenderObject(t *testing.T) {
	o := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "dashboard.grafana.app/v1",
		"kind":       "Dashboard",
		"metadata": map[string]any{
			"name":          "abc",
			"managedFields": []any{map[string]any{"manager": "grafana"}},
		},
		"spec": map[string]any{"title": "Alpha"},
	}}

	got, err := browse.RenderObject(o)
	require.NoError(t, err)
	assert.Contains(t, got, "kind: Dashboard")
	assert.Contains(t, got, "name: abc")
	assert.Contains(t, got, "title: Alpha")
	assert.NotContains(t, got, "managedFields")

	// The caller's object is left intact.
	_, found, _ := unstructured.NestedSlice(o.Object, "metadata", "managedFields")
	assert.True(t, found)

	again, err := browse.RenderObject(o)
	require.NoError(t, err)
	assert.Equal(t, got, again, "output is deterministic")
}
