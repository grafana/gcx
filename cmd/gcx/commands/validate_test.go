package commands_test

import (
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/resources"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCompareAgainstLive_AllCovered(t *testing.T) {
	catalog := []agent.CatalogEntry{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
		{Kind: "Folder", Group: "folder.grafana.app", Version: "v1beta1", Source: "well-known"},
	}

	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
		{GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1beta1"}, Kind: "Folder", Plural: "folders"},
	}

	result := agent.CompareAgainstLive(catalog, live)

	if result.Total != 2 {
		t.Errorf("total = %d, want 2", result.Total)
	}
	if result.Covered != 2 {
		t.Errorf("covered = %d, want 2", result.Covered)
	}
	if len(result.Uncovered) != 0 {
		t.Errorf("uncovered = %d, want 0", len(result.Uncovered))
	}
	if len(result.Stale) != 0 {
		t.Errorf("stale = %d, want 0", len(result.Stale))
	}
}

func TestCompareAgainstLive_UncoveredTypes(t *testing.T) {
	catalog := []agent.CatalogEntry{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
	}

	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
		{GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1beta1"}, Kind: "Folder", Plural: "folders"},
		{GroupVersion: schema.GroupVersion{Group: "playlist.grafana.app", Version: "v1"}, Kind: "Playlist", Plural: "playlists"},
	}

	result := agent.CompareAgainstLive(catalog, live)

	if result.Total != 3 {
		t.Errorf("total = %d, want 3", result.Total)
	}
	if result.Covered != 1 {
		t.Errorf("covered = %d, want 1", result.Covered)
	}
	if len(result.Uncovered) != 2 {
		t.Errorf("uncovered = %d, want 2", len(result.Uncovered))
	}

	// Results are sorted by Kind, so we can check order.
	if len(result.Uncovered) == 2 {
		if result.Uncovered[0].Kind != "Folder" {
			t.Errorf("uncovered[0].Kind = %q, want Folder", result.Uncovered[0].Kind)
		}
		if result.Uncovered[1].Kind != "Playlist" {
			t.Errorf("uncovered[1].Kind = %q, want Playlist", result.Uncovered[1].Kind)
		}
	}
}

func TestCompareAgainstLive_StaleTypes(t *testing.T) {
	catalog := []agent.CatalogEntry{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
		{Kind: "OldThing", Group: "old.grafana.app", Version: "v1", Source: "well-known"},
	}

	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
	}

	result := agent.CompareAgainstLive(catalog, live)

	if result.Covered != 1 {
		t.Errorf("covered = %d, want 1", result.Covered)
	}
	if len(result.Stale) != 1 {
		t.Fatalf("stale = %d, want 1", len(result.Stale))
	}
	if result.Stale[0].Kind != "OldThing" {
		t.Errorf("stale[0].Kind = %q, want OldThing", result.Stale[0].Kind)
	}
}

func TestCompareAgainstLive_DeduplicatesVersions(t *testing.T) {
	catalog := []agent.CatalogEntry{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
	}

	// Same kind/group, different versions — should count as one live type.
	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v0alpha1"}, Kind: "Dashboard", Plural: "dashboards"},
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v2alpha1"}, Kind: "Dashboard", Plural: "dashboards"},
	}

	result := agent.CompareAgainstLive(catalog, live)

	if result.Total != 1 {
		t.Errorf("total = %d, want 1 (should deduplicate versions)", result.Total)
	}
	if result.Covered != 1 {
		t.Errorf("covered = %d, want 1", result.Covered)
	}
	if len(result.Uncovered) != 0 {
		t.Errorf("uncovered = %d, want 0", len(result.Uncovered))
	}
}

func TestCompareAgainstLive_EmptyCatalog(t *testing.T) {
	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
	}

	result := agent.CompareAgainstLive(nil, live)

	if result.Total != 1 {
		t.Errorf("total = %d, want 1", result.Total)
	}
	if len(result.Uncovered) != 1 {
		t.Errorf("uncovered = %d, want 1", len(result.Uncovered))
	}
}

func TestCompareAgainstLive_EmptyLive(t *testing.T) {
	catalog := []agent.CatalogEntry{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
	}

	result := agent.CompareAgainstLive(catalog, nil)

	if result.Total != 0 {
		t.Errorf("total = %d, want 0", result.Total)
	}
	if len(result.Stale) != 1 {
		t.Errorf("stale = %d, want 1", len(result.Stale))
	}
}
