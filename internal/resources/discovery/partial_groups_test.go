package discovery_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const notificationsGroup = "notifications.alerting.grafana.app"

var notificationResources = []string{ //nolint:gochecknoglobals // Test fixture.
	"routingtrees", "receivers", "templategroups", "timeintervals", "inhibitionrules"}

// notificationsDiscovery mimics a Grafana 13 server: v1beta1 preferred,
// v0alpha1 still served, every notification resource in both versions.
func notificationsDiscovery() ([]*metav1.APIGroup, []*metav1.APIResourceList) {
	versions := []string{"v1beta1", "v0alpha1"}
	group := &metav1.APIGroup{Name: notificationsGroup}
	lists := make([]*metav1.APIResourceList, 0, len(versions))
	for _, v := range versions {
		gv := notificationsGroup + "/" + v
		group.Versions = append(group.Versions, metav1.GroupVersionForDiscovery{GroupVersion: gv, Version: v})
		list := &metav1.APIResourceList{GroupVersion: gv}
		for _, name := range notificationResources {
			list.APIResources = append(list.APIResources, metav1.APIResource{
				Name:       name,
				Kind:       name, // kind is irrelevant to filtering
				Namespaced: true,
			})
		}
		list.APIResources = append(list.APIResources, metav1.APIResource{Name: "routingtrees/status", Namespaced: true})
		lists = append(lists, list)
	}
	group.PreferredVersion = group.Versions[0]
	return []*metav1.APIGroup{group}, lists
}

func TestFilterDiscoveryResults_PartiallyExposedGroups(t *testing.T) {
	partial := map[string][]string{notificationsGroup: {"routingtrees"}}

	tests := []struct {
		name          string
		ignored       []string
		partial       map[string][]string
		wantGroup     bool
		wantResources []string
	}{
		{
			name:          "allowlisted resource kept, others dropped",
			partial:       partial,
			wantGroup:     true,
			wantResources: []string{"routingtrees"},
		},
		{
			name:          "no allowlist keeps every resource",
			wantGroup:     true,
			wantResources: notificationResources,
		},
		{
			name:      "ignored group dropped entirely",
			ignored:   []string{notificationsGroup},
			partial:   partial,
			wantGroup: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			groups, lists := notificationsDiscovery()

			gotGroups, gotLists, err := discovery.FilterDiscoveryResults(tc.ignored, tc.partial, groups, lists)
			require.NoError(t, err)

			if !tc.wantGroup {
				assert.Empty(t, gotGroups)
				assert.Empty(t, gotLists)
				return
			}

			require.Len(t, gotGroups, 1)
			assert.Equal(t, notificationsGroup, gotGroups[0].Name)
			assert.Equal(t, "v1beta1", gotGroups[0].PreferredVersion.Version)

			require.Len(t, gotLists, 2)
			for _, list := range gotLists {
				var names []string
				for _, r := range list.APIResources {
					names = append(names, r.Name)
				}
				assert.ElementsMatch(t, tc.wantResources, names, list.GroupVersion)
			}
		})
	}
}

func TestRegistry_NotificationsGroupExposesOnlyRoutingTrees(t *testing.T) {
	groups, lists := notificationsDiscovery()
	reg, err := discovery.NewCachedRegistry(t.Context(), &mockDiscoveryClient{groups: groups, resources: lists})
	require.NoError(t, err)

	lookup := func(version, resource string) (resources.Filters, error) {
		return reg.MakeFilters(discovery.MakeFiltersOptions{
			Selectors: resources.Selectors{{
				Type:             resources.FilterTypeAll,
				GroupVersionKind: resources.PartialGVK{Group: notificationsGroup, Version: version, Resource: resource},
			}},
			PreferredVersionOnly: true,
		})
	}

	t.Run("routingtrees resolves to preferred version", func(t *testing.T) {
		filters, err := lookup("", "routingtrees")
		require.NoError(t, err)
		require.Len(t, filters, 1)
		assert.Equal(t, "v1beta1", filters[0].Descriptor.GroupVersion.Version)
	})

	t.Run("routingtrees resolves an explicit older version", func(t *testing.T) {
		filters, err := lookup("v0alpha1", "routingtrees")
		require.NoError(t, err)
		require.Len(t, filters, 1)
		assert.Equal(t, "v0alpha1", filters[0].Descriptor.GroupVersion.Version)
	})

	for _, hidden := range []string{"receivers", "templategroups", "timeintervals", "inhibitionrules"} {
		t.Run(hidden+" is not resolvable", func(t *testing.T) {
			_, err := lookup("", hidden)
			require.Error(t, err)
			_, err = lookup("v1beta1", hidden)
			require.Error(t, err)
		})
	}

	t.Run("preferred resources list only routingtrees from the group", func(t *testing.T) {
		var plurals []string
		for _, d := range reg.PreferredResources() {
			if d.GroupVersion.Group == notificationsGroup {
				plurals = append(plurals, d.Plural)
			}
		}
		assert.Equal(t, []string{"routingtrees"}, plurals)
	})
}
