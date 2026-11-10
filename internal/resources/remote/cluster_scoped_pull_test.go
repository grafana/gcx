package remote_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestPull_AllSkipsClusterScopedKinds(t *testing.T) {
	dash := dashboardDescriptor()
	cluster := resources.Descriptor{
		GroupVersion:  schema.GroupVersion{Group: "kgatlas.ext.grafana.app", Version: "v1alpha1"},
		Kind:          "SystemKgSchema",
		Singular:      "systemkgschema",
		Plural:        "systemkgschemas",
		ClusterScoped: true,
	}
	schemaObj := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kgatlas.ext.grafana.app/v1alpha1",
		"kind":       "SystemKgSchema",
		"metadata":   map[string]any{"name": "core"},
	}}
	client := &mockPullClient{listResults: map[string][]unstructured.Unstructured{
		dash.Plural:    {makeUnstructuredDashboard("dash")},
		cluster.Plural: {schemaObj},
	}}
	puller := remote.NewPuller(client, &mockPullRegistry{descriptors: resources.Descriptors{dash, cluster}})

	t.Run("no selector pulls only namespaced kinds", func(t *testing.T) {
		var dest resources.Resources
		_, err := puller.Pull(t.Context(), remote.PullRequest{Resources: &dest})
		require.NoError(t, err)
		require.Equal(t, 1, dest.Len())
		require.Equal(t, "Dashboard", dest.AsList()[0].Kind())
	})

	t.Run("selector pulls the cluster-scoped kind", func(t *testing.T) {
		var dest resources.Resources
		_, err := puller.Pull(t.Context(), remote.PullRequest{
			Resources: &dest,
			Filters:   resources.Filters{{Type: resources.FilterTypeAll, Descriptor: cluster}},
		})
		require.NoError(t, err)
		require.Equal(t, 1, dest.Len())
		require.Equal(t, "SystemKgSchema", dest.AsList()[0].Kind())
	})
}
