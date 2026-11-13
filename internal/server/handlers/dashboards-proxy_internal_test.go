package handlers

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/stretchr/testify/require"
)

func dashboardResource(t *testing.T, version, name string) *resources.Resource {
	t.Helper()

	return resources.MustFromObject(map[string]any{
		"apiVersion": "dashboard.grafana.app/" + version,
		"kind":       "Dashboard",
		"metadata": map[string]any{
			"name":      name,
			"namespace": "default",
		},
		"spec": map[string]any{},
	}, resources.SourceInfo{})
}

func TestPreferredDashboardVersion_NoneLoaded(t *testing.T) {
	proxy := NewDashboardProxy(nil, resources.NewResources())
	require.Equal(t, "v2", proxy.preferredDashboardVersion())
}

func TestPreferredDashboardVersion_SingleVersionDirectory(t *testing.T) {
	rs := resources.NewResources(
		dashboardResource(t, "v2beta1", "a"),
		dashboardResource(t, "v2beta1", "b"),
		dashboardResource(t, "v2beta1", "c"),
	)
	proxy := NewDashboardProxy(nil, rs)
	require.Equal(t, "v2beta1", proxy.preferredDashboardVersion())
}

func TestPreferredDashboardVersion_MixedPicksMajority(t *testing.T) {
	rs := resources.NewResources(
		dashboardResource(t, "v2alpha1", "a"),
		dashboardResource(t, "v2", "b"),
		dashboardResource(t, "v2", "c"),
	)
	proxy := NewDashboardProxy(nil, rs)
	require.Equal(t, "v2", proxy.preferredDashboardVersion())
}

func TestPreferredDashboardVersion_IgnoresNonDashboardKinds(t *testing.T) {
	folder := resources.MustFromObject(map[string]any{
		"apiVersion": "folder.grafana.app/v1beta1",
		"kind":       "Folder",
		"metadata": map[string]any{
			"name":      "f",
			"namespace": "default",
		},
	}, resources.SourceInfo{})

	rs := resources.NewResources(folder, dashboardResource(t, "v2", "a"))
	proxy := NewDashboardProxy(nil, rs)
	require.Equal(t, "v2", proxy.preferredDashboardVersion())
}

func TestPreferredDashboardVersion_TieIsDeterministic(t *testing.T) {
	rs := resources.NewResources(
		dashboardResource(t, "v2beta1", "a"),
		dashboardResource(t, "v2", "b"),
	)
	proxy := NewDashboardProxy(nil, rs)

	// AsList() sorts by (group, version, kind, name); "v2" < "v2beta1"
	// lexicographically, so resource "b" (v2) is seen before "a" (v2beta1).
	// Tied at one each, first-seen wins deterministically. Re-running this
	// against Resources.ForEach's raw map iteration would flake
	// intermittently.
	for range 20 {
		require.Equal(t, "v2", proxy.preferredDashboardVersion())
	}
}

func TestDashboardAPIGroupVersions_UnionsUnknownPreferred(t *testing.T) {
	versions := dashboardAPIGroupVersions("v3")
	require.Equal(t, "v3", versions[0]["version"])
	require.Equal(t, "dashboard.grafana.app/v3", versions[0]["groupVersion"])
	require.Len(t, versions, 7) // the known six, plus v3

	var found bool
	for _, v := range versions {
		if v["version"] == "v2" {
			found = true
		}
	}
	require.True(t, found, "known versions must still be listed")
}

func TestDashboardAPIGroupVersions_KnownPreferredMovesToFront(t *testing.T) {
	versions := dashboardAPIGroupVersions("v2beta1")
	require.Equal(t, "v2beta1", versions[0]["version"])
	require.Len(t, versions, 6) // no duplicate entry
}
