package resources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNativeCollectionReferenceScoping(t *testing.T) {
	makeResource := func(version, kind, namespace, apiName string, spec map[string]any) *resources.Resource {
		return resources.MustFromUnstructured(&unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "notifications.alerting.grafana.app/" + version, "kind": kind,
			"metadata": map[string]any{"namespace": namespace, "name": apiName}, "spec": spec,
		}})
	}
	base := makeResource("v0alpha1", "TemplateGroup", "target", "", map[string]any{"title": "same"})

	for _, tc := range []struct {
		name string
		peer *resources.Resource
	}{
		{"different namespace", makeResource("v0alpha1", "TemplateGroup", "other", "", map[string]any{"title": "same"})},
		{"different API version", makeResource("v1beta1", "TemplateGroup", "target", "", map[string]any{"title": "same"})},
		{"different resource kind", makeResource("v0alpha1", "TimeInterval", "target", "", map[string]any{"name": "same"})},
		{"different template kind", makeResource("v0alpha1", "TemplateGroup", "target", "", map[string]any{"title": "same", "kind": "mimir"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEqual(t, base.Ref(), tc.peer.Ref())
			require.Equal(t, 2, resources.NewResources(base, tc.peer).Len())
		})
	}

	t.Run("historical kind and explicit grafana deduplicate", func(t *testing.T) {
		explicit := makeResource("v0alpha1", "TemplateGroup", "target", "", map[string]any{"title": "same", "kind": "grafana"})
		require.Equal(t, base.Ref(), explicit.Ref())
		require.Equal(t, 1, resources.NewResources(base, explicit).Len())
	})

	t.Run("source-less invalid instance key is stable and distinct", func(t *testing.T) {
		first := makeResource("v0alpha1", "TimeInterval", "target", "", map[string]any{"name": ""})
		second := makeResource("v0alpha1", "TimeInterval", "target", "", map[string]any{"name": ""})
		firstRef := first.Ref()
		require.Equal(t, firstRef, first.Ref())
		require.NotEqual(t, firstRef, second.Ref())
		collection := resources.NewResources(first)
		collection.Add(first)
		require.Equal(t, 1, collection.Len(), "adding the same malformed instance must remain idempotent")
		collection.Add(second)
		require.Equal(t, 2, collection.Len(), "distinct malformed inputs must remain individually reportable")
	})

	t.Run("invalid file keys deduplicate repeated reads", func(t *testing.T) {
		first := makeResource("v0alpha1", "TimeInterval", "target", "", map[string]any{"name": ""})
		repeated := makeResource("v0alpha1", "TimeInterval", "target", "", map[string]any{"name": ""})
		other := makeResource("v0alpha1", "TimeInterval", "target", "", map[string]any{"name": ""})
		first.SetSource(resources.SourceInfo{Path: "/resources/first.yaml"})
		repeated.SetSource(resources.SourceInfo{Path: "/resources/first.yaml"})
		other.SetSource(resources.SourceInfo{Path: "/resources/other.yaml"})

		require.Equal(t, first.Ref(), repeated.Ref(), "rereading an invalid file must produce the same reference")
		require.Contains(t, string(first.Ref()), first.SourcePath(), "the reference must identify the invalid source")
		require.NotEqual(t, first.Ref(), other.Ref())
		collection := resources.NewResources(first, repeated)
		require.Equal(t, 1, collection.Len())
		collection.Add(other)
		require.Equal(t, 2, collection.Len(), "different invalid files must remain individually reportable")
	})

	t.Run("named key exact representation unchanged", func(t *testing.T) {
		named := makeResource("v0alpha1", "TemplateGroup", "target", "chosen-address", map[string]any{"title": "same"})
		require.Equal(t, resources.ResourceRef("notifications.alerting.grafana.app/v0alpha1, Kind=TemplateGroup/target-chosen-address"), named.Ref())
		otherTitle := makeResource("v0alpha1", "TemplateGroup", "target", "chosen-address", map[string]any{"title": "other"})
		require.Equal(t, named.Ref(), otherTitle.Ref())
		require.Equal(t, 1, resources.NewResources(named, otherTitle).Len())
	})
}
