package resources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNativeIdentity(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, tc := range []struct {
			name, kind        string
			spec              map[string]any
			identity          string
			readOnly, invalid bool
		}{
			{"interval", "TimeInterval", map[string]any{"name": "weekends"}, "weekends", false, false},
			{"historical", "TemplateGroup", map[string]any{"title": "foo", "content": "data"}, "grafana\x00foo", false, false},
			{"editable", "TemplateGroup", map[string]any{"title": "foo", "kind": "grafana"}, "grafana\x00foo", false, false},
			{"import", "TemplateGroup", map[string]any{"title": "foo", "kind": "mimir"}, "mimir\x00foo", true, false},
			{"empty title", "TimeInterval", map[string]any{"name": ""}, "", false, true},
			{"missing title", "TemplateGroup", map[string]any{}, "", false, true},
			{"malformed title", "TimeInterval", map[string]any{"name": int64(4)}, "", false, true},
			{"empty kind", "TemplateGroup", map[string]any{"title": "foo", "kind": ""}, "", false, true},
			{"malformed kind", "TemplateGroup", map[string]any{"title": "foo", "kind": true}, "", false, true},
			{"unknown kind", "TemplateGroup", map[string]any{"title": "foo", "kind": "other"}, "", false, true},
		} {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				obj := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "notifications.alerting.grafana.app/" + version, "kind": tc.kind, "spec": tc.spec,
				}}
				original := obj.DeepCopy()
				identity, readOnly, err := resources.NativeIdentity(obj)
				if tc.invalid {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.identity, identity)
					require.Equal(t, tc.readOnly, readOnly)
				}
				require.Equal(t, original, obj)
			})
		}
	}
}

func TestNativePolicyNonOptedIn(t *testing.T) {
	for _, apiVersion := range []string{"dashboard.grafana.app/v1beta1", "other.app/v1"} {
		obj := &unstructured.Unstructured{Object: map[string]any{"apiVersion": apiVersion, "kind": "TimeInterval", "metadata": map[string]any{"name": "chosen", "resourceVersion": "7"}}}
		require.False(t, resources.HasServerDerivedName(obj.GroupVersionKind()))
		prepared := resources.PrepareNativeCreate(obj)
		require.Equal(t, obj, prepared)
		prepared.SetName("different")
		require.Equal(t, "chosen", obj.GetName())
	}
}

func TestPrepareNativeCreate(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, tc := range []struct {
			name, kind string
			spec       map[string]any
		}{
			{"interval", "TimeInterval", map[string]any{"name": "weekends"}},
			{"historical", "TemplateGroup", map[string]any{"title": "foo", "content": "data"}},
			{"editable", "TemplateGroup", map[string]any{"title": "foo", "kind": "grafana"}},
			{"read-only", "TemplateGroup", map[string]any{"title": "foo", "kind": "mimir"}},
			{"invalid identity", "TimeInterval", map[string]any{"name": int64(4)}},
			{"invalid kind", "TemplateGroup", map[string]any{"title": "foo", "kind": true}},
		} {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				obj := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": "notifications.alerting.grafana.app/" + version, "kind": tc.kind, "spec": tc.spec,
					"metadata": map[string]any{
						"name": "old", "generateName": "old-", "uid": "uid", "resourceVersion": "7", "generation": int64(3),
						"creationTimestamp": "2026-01-01T00:00:00Z", "deletionTimestamp": "2026-01-01T00:00:00Z",
						"deletionGracePeriodSeconds": int64(3), "managedFields": []any{}, "namespace": "target",
						"labels": map[string]any{"x": "y"}, "annotations": map[string]any{"grafana.com/provenance": "api"},
					},
				}}
				original := obj.DeepCopy()
				prepared := resources.PrepareNativeCreate(obj)
				require.Equal(t, tc.spec, prepared.Object["spec"])
				require.Equal(t, "target", prepared.GetNamespace())
				require.Equal(t, obj.GetAnnotations(), prepared.GetAnnotations())
				require.Equal(t, obj.GetLabels(), prepared.GetLabels())
				for _, field := range []string{"name", "generateName", "uid", "resourceVersion", "generation", "creationTimestamp", "deletionTimestamp", "deletionGracePeriodSeconds", "managedFields"} {
					_, found, _ := unstructured.NestedFieldNoCopy(prepared.Object, "metadata", field)
					require.False(t, found, field)
				}
				require.Equal(t, original, obj)
				spec, ok := prepared.Object["spec"].(map[string]any)
				require.True(t, ok)
				spec["new"] = "changed"
				prepared.SetLabels(map[string]string{"x": "changed"})
				require.Equal(t, original, obj, "the prepared copy must not share source spec or metadata")
			})
		}
	}
}
