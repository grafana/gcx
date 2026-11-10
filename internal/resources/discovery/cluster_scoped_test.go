package discovery_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const kgGroup = "kgatlas.ext.grafana.app"

// kgDiscovery mimics a group that serves namespaced and cluster-scoped kinds.
func kgDiscovery() ([]*metav1.APIGroup, []*metav1.APIResourceList) {
	gv := metav1.GroupVersionForDiscovery{GroupVersion: kgGroup + "/v1alpha1", Version: "v1alpha1"}
	groups := []*metav1.APIGroup{{Name: kgGroup, Versions: []metav1.GroupVersionForDiscovery{gv}, PreferredVersion: gv}}
	lists := []*metav1.APIResourceList{{
		GroupVersion: gv.GroupVersion,
		APIResources: []metav1.APIResource{
			{Name: "kgschemas", SingularName: "kgschema", Kind: "KgSchema", Namespaced: true},
			{Name: "systemkgschemas", SingularName: "systemkgschema", Kind: "SystemKgSchema", Namespaced: false},
			{Name: "systemkgschemas/status", Kind: "SystemKgSchema", Namespaced: false},
		},
	}}
	return groups, lists
}

func TestFilterDiscoveryResults_KeepsClusterScopedResources(t *testing.T) {
	groups, lists := kgDiscovery()

	_, filtered, err := discovery.FilterDiscoveryResults(nil, nil, groups, lists)
	require.NoError(t, err)
	require.Len(t, filtered, 1)

	names := make([]string, 0, len(filtered[0].APIResources))
	for _, r := range filtered[0].APIResources {
		names = append(names, r.Name)
	}
	// Subresources stay excluded for both scopes.
	assert.Equal(t, []string{"kgschemas", "systemkgschemas"}, names)
}

func TestRegistry_ClusterScopedDescriptors(t *testing.T) {
	groups, lists := kgDiscovery()
	reg, err := discovery.NewRegistry(t.Context(), &mockDiscoveryClient{groups: groups, resources: lists})
	require.NoError(t, err)

	scopes := make(map[string]string)
	for _, d := range reg.SupportedResources() {
		scopes[d.Plural] = d.Scope()
	}
	assert.Equal(t, map[string]string{
		"kgschemas":       resources.ScopeNamespaced,
		"systemkgschemas": resources.ScopeCluster,
	}, scopes)

	sels, err := resources.ParseSelectors([]string{"systemkgschemas/default"})
	require.NoError(t, err)
	filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{Selectors: sels, PreferredVersionOnly: true})
	require.NoError(t, err)
	require.Len(t, filters, 1)
	assert.True(t, filters[0].Descriptor.ClusterScoped)
}
