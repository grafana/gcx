package dynamic_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
)

func clusterScopedDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion:  schema.GroupVersion{Group: "kgatlas.ext.grafana.app", Version: "v1alpha1"},
		Kind:          "SystemKgSchema",
		Singular:      "systemkgschema",
		Plural:        "systemkgschemas",
		ClusterScoped: true,
	}
}

func newScopedFakeClient(t *testing.T, objects ...runtime.Object) *fake.FakeDynamicClient {
	t.Helper()

	scheme := runtime.NewScheme()
	for _, desc := range []resources.Descriptor{testDescriptor(), clusterScopedDescriptor()} {
		gvk := desc.GroupVersionKind()
		scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
		scheme.AddKnownTypeWithName(gvk.GroupVersion().WithKind(gvk.Kind+"List"), &unstructured.UnstructuredList{})
	}

	return fake.NewSimpleDynamicClient(scheme, objects...)
}

func TestNamespacedClient_RequestScope(t *testing.T) {
	schemaObj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kgatlas.ext.grafana.app/v1alpha1",
		"kind":       "SystemKgSchema",
		"metadata":   map[string]any{"name": "core"},
	}}
	client := newScopedFakeClient(t, schemaObj, testDashboard("dash"))
	c := dynamic.NewNamespacedClient("default", client)

	tests := []struct {
		name          string
		desc          resources.Descriptor
		object        string
		wantNamespace string
	}{
		{name: "cluster-scoped kind omits the namespace", desc: clusterScopedDescriptor(), object: "core", wantNamespace: ""},
		{name: "namespaced kind keeps the namespace", desc: testDescriptor(), object: "dash", wantNamespace: "default"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client.ClearActions()

			list, err := c.List(t.Context(), tc.desc, metav1.ListOptions{Limit: 10})
			require.NoError(t, err)
			require.Len(t, list.Items, 1)

			obj, err := c.Get(t.Context(), tc.desc, tc.object, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, tc.object, obj.GetName())

			actions := client.Actions()
			require.Len(t, actions, 2)
			for _, action := range actions {
				assert.Equal(t, tc.wantNamespace, action.GetNamespace(), action.GetVerb())
			}
		})
	}
}
