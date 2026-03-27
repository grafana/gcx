package commands_test

import (
	"testing"

	"github.com/grafana/gcx/cmd/gcx/commands"
	"github.com/grafana/gcx/internal/resources"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestCompareAgainstLive_AllCovered(t *testing.T) {
	catalog := []commands.ResourceTypeInfo{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
		{Kind: "Folder", Group: "folder.grafana.app", Version: "v1beta1", Source: "well-known"},
	}

	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
		{GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1beta1"}, Kind: "Folder", Plural: "folders"},
	}

	result := commands.ExportCompareAgainstLive(catalog, live)

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
	catalog := []commands.ResourceTypeInfo{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
	}

	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
		{GroupVersion: schema.GroupVersion{Group: "folder.grafana.app", Version: "v1beta1"}, Kind: "Folder", Plural: "folders"},
		{GroupVersion: schema.GroupVersion{Group: "playlist.grafana.app", Version: "v1"}, Kind: "Playlist", Plural: "playlists"},
	}

	result := commands.ExportCompareAgainstLive(catalog, live)

	if result.Total != 3 {
		t.Errorf("total = %d, want 3", result.Total)
	}
	if result.Covered != 1 {
		t.Errorf("covered = %d, want 1", result.Covered)
	}
	if len(result.Uncovered) != 2 {
		t.Errorf("uncovered = %d, want 2", len(result.Uncovered))
	}

	uncoveredKinds := map[string]bool{}
	for _, u := range result.Uncovered {
		uncoveredKinds[u.Kind] = true
	}
	if !uncoveredKinds["Folder"] {
		t.Error("expected Folder in uncovered")
	}
	if !uncoveredKinds["Playlist"] {
		t.Error("expected Playlist in uncovered")
	}
}

func TestCompareAgainstLive_StaleTypes(t *testing.T) {
	catalog := []commands.ResourceTypeInfo{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
		{Kind: "OldThing", Group: "old.grafana.app", Version: "v1", Source: "well-known"},
	}

	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
	}

	result := commands.ExportCompareAgainstLive(catalog, live)

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
	catalog := []commands.ResourceTypeInfo{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
	}

	// Same kind/group, different versions — should count as one live type.
	live := resources.Descriptors{
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v0alpha1"}, Kind: "Dashboard", Plural: "dashboards"},
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v1beta1"}, Kind: "Dashboard", Plural: "dashboards"},
		{GroupVersion: schema.GroupVersion{Group: "dashboard.grafana.app", Version: "v2alpha1"}, Kind: "Dashboard", Plural: "dashboards"},
	}

	result := commands.ExportCompareAgainstLive(catalog, live)

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

	result := commands.ExportCompareAgainstLive(nil, live)

	if result.Total != 1 {
		t.Errorf("total = %d, want 1", result.Total)
	}
	if len(result.Uncovered) != 1 {
		t.Errorf("uncovered = %d, want 1", len(result.Uncovered))
	}
}

func TestCompareAgainstLive_EmptyLive(t *testing.T) {
	catalog := []commands.ResourceTypeInfo{
		{Kind: "Dashboard", Group: "dashboard.grafana.app", Version: "v1beta1", Source: "well-known"},
	}

	result := commands.ExportCompareAgainstLive(catalog, nil)

	if result.Total != 0 {
		t.Errorf("total = %d, want 0", result.Total)
	}
	if len(result.Stale) != 1 {
		t.Errorf("stale = %d, want 1", len(result.Stale))
	}
}
