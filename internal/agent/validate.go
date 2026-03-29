package agent

import (
	"sort"

	"github.com/grafana/gcx/internal/resources"
)

// ValidationResult holds the result of comparing the catalog against live discovery.
type ValidationResult struct {
	Uncovered []UncoveredType `json:"uncovered"` // live types not in catalog
	Stale     []StaleType     `json:"stale"`     // catalog types not found live
	Covered   int             `json:"covered"`   // catalog types confirmed live
	Total     int             `json:"total"`     // total live types discovered
}

// UncoveredType is a live resource type missing from the catalog.
type UncoveredType struct {
	Kind    string `json:"kind"`
	Group   string `json:"group"`
	Version string `json:"version"`
	Plural  string `json:"plural"`
}

// StaleType is a catalog resource type not found on the live instance.
type StaleType struct {
	Kind    string `json:"kind"`
	Group   string `json:"group"`
	Version string `json:"version"`
	Source  string `json:"source"` // "well-known" or "adapter"
}

// CatalogEntry represents a catalog resource type for validation comparison.
type CatalogEntry struct {
	Kind    string
	Group   string
	Version string
	Source  string
}

// CompareAgainstLive is the pure comparison logic, separated for testing.
func CompareAgainstLive(catalog []CatalogEntry, liveDescs resources.Descriptors) *ValidationResult {
	// Build a set of cataloged types keyed by kind+group.
	cataloged := make(map[string]CatalogEntry, len(catalog))
	for _, rt := range catalog {
		key := rt.Kind + "/" + rt.Group
		cataloged[key] = rt
	}

	// Build a set of live types, deduplicating by kind+group across versions.
	live := make(map[string]resources.Descriptor, len(liveDescs))
	for _, d := range liveDescs {
		key := d.Kind + "/" + d.GroupVersion.Group
		if _, exists := live[key]; !exists {
			live[key] = d
		}
	}

	result := &ValidationResult{Total: len(live)}

	// Find uncovered: live types not in catalog.
	for key, desc := range live {
		if _, ok := cataloged[key]; ok {
			result.Covered++
		} else {
			result.Uncovered = append(result.Uncovered, UncoveredType{
				Kind:    desc.Kind,
				Group:   desc.GroupVersion.Group,
				Version: desc.GroupVersion.Version,
				Plural:  desc.Plural,
			})
		}
	}

	// Find stale: catalog types not found live.
	for _, rt := range catalog {
		key := rt.Kind + "/" + rt.Group
		if _, ok := live[key]; !ok {
			result.Stale = append(result.Stale, StaleType(rt))
		}
	}

	// Sort for deterministic output.
	sort.Slice(result.Uncovered, func(i, j int) bool {
		if result.Uncovered[i].Kind != result.Uncovered[j].Kind {
			return result.Uncovered[i].Kind < result.Uncovered[j].Kind
		}
		return result.Uncovered[i].Group < result.Uncovered[j].Group
	})
	sort.Slice(result.Stale, func(i, j int) bool {
		if result.Stale[i].Kind != result.Stale[j].Kind {
			return result.Stale[i].Kind < result.Stale[j].Kind
		}
		return result.Stale[i].Group < result.Stale[j].Group
	})

	return result
}
