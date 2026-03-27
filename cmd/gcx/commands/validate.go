package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
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

// validateAgainstLive compares the catalog resource types against a live Grafana instance.
func validateAgainstLive(ctx context.Context, cfg config.NamespacedRESTConfig, catalog []ResourceTypeInfo) (*ValidationResult, error) {
	reg, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to discover resources: %w", err)
	}

	liveDescs := reg.SupportedResources().Sorted()

	return compareAgainstLive(catalog, liveDescs), nil
}

// compareAgainstLive is the pure comparison logic, separated for testing.
func compareAgainstLive(catalog []ResourceTypeInfo, liveDescs resources.Descriptors) *ValidationResult {
	// Build a set of cataloged types keyed by kind+group.
	cataloged := make(map[string]ResourceTypeInfo, len(catalog))
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
			result.Stale = append(result.Stale, StaleType{
				Kind:    rt.Kind,
				Group:   rt.Group,
				Version: rt.Version,
				Source:  rt.Source,
			})
		}
	}

	return result
}

// writeValidationReport outputs a human-readable validation report.
func writeValidationReport(w io.Writer, result *ValidationResult) {
	fmt.Fprintf(w, "Resource type catalog validation\n")
	fmt.Fprintf(w, "================================\n\n")
	fmt.Fprintf(w, "Live types discovered:  %d\n", result.Total)
	fmt.Fprintf(w, "Catalog coverage:       %d/%d\n", result.Covered, result.Total)
	fmt.Fprintf(w, "Uncovered (live only):  %d\n", len(result.Uncovered))
	fmt.Fprintf(w, "Stale (catalog only):   %d\n\n", len(result.Stale))

	if len(result.Uncovered) > 0 {
		fmt.Fprintf(w, "Uncovered types (add to well-known or adapter registry):\n")
		for _, u := range result.Uncovered {
			fmt.Fprintf(w, "  %-35s %s/%s\n", u.Kind, u.Group, u.Version)
		}
		fmt.Fprintln(w)
	}

	if len(result.Stale) > 0 {
		fmt.Fprintf(w, "Stale types (no longer on live instance):\n")
		for _, s := range result.Stale {
			fmt.Fprintf(w, "  %-35s %s/%s  [%s]\n", s.Kind, s.Group, s.Version, s.Source)
		}
		fmt.Fprintln(w)
	}

	if len(result.Uncovered) == 0 && len(result.Stale) == 0 {
		fmt.Fprintf(w, "All catalog entries verified against live instance.\n")
	}
}
