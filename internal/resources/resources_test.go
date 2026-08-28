package resources_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
)

func dashboardNamed(name string) *resources.Resource {
	return resources.MustFromObject(map[string]any{
		"apiVersion": "dashboard.grafana.app/v1",
		"kind":       "Dashboard",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"uid": name,
		},
	}, resources.SourceInfo{})
}

func folderNamed(name string) *resources.Resource {
	return resources.MustFromObject(map[string]any{
		"apiVersion": "folder.grafana.app/v1",
		"kind":       "Folder",
		"metadata": map[string]any{
			"name": name,
		},
		"spec": map[string]any{
			"title": name,
		},
	}, resources.SourceInfo{})
}

func TestResources_AsList_SortedAndDeterministic(t *testing.T) {
	// Add resources out of order, and interleave kinds, to exercise the sort.
	inputs := []*resources.Resource{
		dashboardNamed("charlie"),
		folderNamed("beta"),
		dashboardNamed("alpha"),
		folderNamed("alpha"),
		dashboardNamed("bravo"),
	}

	// group < version < kind < name: dashboard.grafana.app sorts before
	// folder.grafana.app, then names alphabetically within each kind.
	want := []string{"alpha", "bravo", "charlie", "alpha", "beta"}

	// Build the collection from a fresh map order on each iteration to prove the
	// output does not depend on Go's randomized map iteration order.
	for i := range 50 {
		coll := resources.NewResources(inputs...)

		list := coll.AsList()
		if len(list) != len(want) {
			t.Fatalf("got %d resources, want %d", len(list), len(want))
		}

		got := make([]string, 0, len(list))
		for _, r := range list {
			got = append(got, r.Name())
		}

		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("iteration %d: got order %v, want %v", i, got, want)
			}
		}
	}
}

func TestResources_AsList_NilCollection(t *testing.T) {
	coll := &resources.Resources{}
	if list := coll.AsList(); list != nil {
		t.Fatalf("expected nil list for nil collection, got %v", list)
	}
}
