package commands

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/discovery"
)

// validateAgainstLive compares the catalog resource types against a live Grafana instance.
func validateAgainstLive(ctx context.Context, cfg config.NamespacedRESTConfig, catalog []ResourceTypeInfo) (*agent.ValidationResult, error) {
	reg, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to discover resources: %w", err)
	}

	liveDescs := reg.SupportedResources().Sorted()

	entries := make([]agent.CatalogEntry, len(catalog))
	for i, rt := range catalog {
		entries[i] = agent.CatalogEntry{
			Kind:    rt.Kind,
			Group:   rt.Group,
			Version: rt.Version,
			Source:  rt.Source,
		}
	}

	return agent.CompareAgainstLive(entries, liveDescs), nil
}

// writeValidationReport outputs a human-readable validation report.
func writeValidationReport(w io.Writer, result *agent.ValidationResult) {
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
