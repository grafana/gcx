package grafanameta_test

import (
	"testing"

	"github.com/grafana/gcx/internal/grafanameta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestManagerProperties(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{}}
	obj.SetAnnotations(map[string]string{"grafana.app/repoName": "legacy"})
	acc, err := grafanameta.MetaAccessor(obj)
	require.NoError(t, err)

	// The older key still reads as a repo manager.
	got, ok := acc.GetManagerProperties()
	assert.True(t, ok)
	assert.Equal(t, grafanameta.ManagerProperties{Kind: grafanameta.ManagerKindRepo, Identity: "legacy"}, got)

	want := grafanameta.ManagerProperties{Kind: grafanameta.ManagerKindKubectl, Identity: "gcx", Suspended: true}
	acc.SetManagerProperties(want)
	got, ok = acc.GetManagerProperties()
	assert.True(t, ok)
	assert.Equal(t, want, got)
	// Written to the object itself, zero fields left out, the older key dropped.
	assert.Equal(t, map[string]string{
		grafanameta.AnnoKeyManagerKind:      "kubectl",
		grafanameta.AnnoKeyManagerIdentity:  "gcx",
		grafanameta.AnnoKeyManagerSuspended: "true",
	}, obj.GetAnnotations())

	acc.SetManagerProperties(grafanameta.ManagerProperties{})
	_, ok = acc.GetManagerProperties()
	assert.False(t, ok)
	assert.Empty(t, obj.GetAnnotations())

	obj.SetAnnotations(map[string]string{grafanameta.AnnoKeyManagerIdentity: "x", grafanameta.AnnoKeyManagerKind: "other"})
	got, _ = acc.GetManagerProperties()
	assert.Equal(t, grafanameta.ManagerKindUnknown, got.Kind)
}

func TestSourcePropertiesAndSpec(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"name": "n"}}}
	acc, err := grafanameta.MetaAccessor(obj)
	require.NoError(t, err)

	acc.SetSourceProperties(grafanameta.SourceProperties{Path: "a.yaml", TimestampMillis: 42})
	assert.Equal(t, map[string]string{
		grafanameta.AnnoKeySourcePath:      "a.yaml",
		grafanameta.AnnoKeySourceTimestamp: "42",
	}, obj.GetAnnotations())

	assert.Equal(t, "n", acc.FindTitle("default"))
	require.NoError(t, unstructured.SetNestedField(obj.Object, "t", "spec", "title"))
	assert.Equal(t, "t", acc.FindTitle("default"))

	_, err = acc.GetStatus()
	require.Error(t, err)
	_, err = grafanameta.MetaAccessor(nil)
	require.Error(t, err)
}
