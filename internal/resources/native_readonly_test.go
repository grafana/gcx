package resources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNativeReadOnlyPolicy(t *testing.T) {
	for _, version := range []string{"v0alpha1", "v1beta1"} {
		for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
			for _, tc := range []struct {
				name, provenance, sourceKind string
				readOnly                     bool
			}{
				{name: "system", provenance: "system", readOnly: true},
				{name: "converted", provenance: "converted_prometheus", readOnly: true},
				{name: "file", provenance: "file", readOnly: true},
				{name: "imported", sourceKind: "mimir", readOnly: true},
				{name: "api", provenance: "api"},
				{name: "absent"},
			} {
				t.Run(version+"/"+kind+"/"+tc.name, func(t *testing.T) {
					spec := map[string]any{"name": "weekend"}
					if kind == "TemplateGroup" {
						spec = map[string]any{"title": "weekend"}
					}
					if tc.sourceKind != "" {
						spec["kind"] = tc.sourceKind
					}
					obj := &unstructured.Unstructured{Object: map[string]any{
						"apiVersion": "notifications.alerting.grafana.app/" + version, "kind": kind,
						"metadata": map[string]any{"name": "opaque"}, "spec": spec,
					}}
					if tc.provenance != "" {
						obj.SetAnnotations(map[string]string{"grafana.com/provenance": tc.provenance})
					}
					before := obj.DeepCopy()
					require.Equal(t, tc.readOnly, resources.IsReadOnlyNative(obj))
					_, readOnly, err := resources.NativeIdentity(obj)
					require.NoError(t, err)
					require.Equal(t, tc.readOnly, readOnly)
					if tc.readOnly {
						err = resources.ReadOnlyNativeError(obj)
						require.ErrorContains(t, err, "read-only")
						require.ErrorContains(t, err, kind)
						require.ErrorContains(t, err, "weekend")
						guidance := map[string]string{"system": "built into Grafana", "converted_prometheus": "external Alertmanager configuration", "file": "provisioning file", "mimir": "external Alertmanager configuration"}
						reason := tc.provenance
						if reason == "" {
							reason = tc.sourceKind
						}
						require.ErrorContains(t, err, guidance[reason])
					}
					require.Equal(t, before, obj)
				})
			}
		}
	}
}

func TestNativeReadOnlyPolicy_NonOptedKindsUnaffected(t *testing.T) {
	for _, tc := range []struct{ apiVersion, kind string }{
		{"other.app/v1", "TimeInterval"},
		{"other.app/v1", "TemplateGroup"},
		{"notifications.alerting.grafana.app/v1beta1", "RoutingTree"},
	} {
		t.Run(tc.apiVersion+"/"+tc.kind, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": tc.apiVersion, "kind": tc.kind, "spec": map[string]any{"kind": "mimir"},
				"metadata": map[string]any{"name": "name", "annotations": map[string]any{"grafana.com/provenance": "system"}},
			}}
			require.False(t, resources.IsReadOnlyNative(obj))
			prepared := resources.PrepareNativeCreate(obj)
			require.Equal(t, obj, prepared)
		})
	}
}
