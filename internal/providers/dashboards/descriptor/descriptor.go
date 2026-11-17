// Package descriptor provides shared dashboard resource descriptor resolution.
package descriptor

import (
	"context"
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Resolve resolves the Kubernetes resource descriptor for the dashboard
// resource using the given REST config and optional API version override.
//
// When apiVersion is non-empty it is parsed directly into a GroupVersion, and
// the selector is constructed with that specific version so discovery returns
// exactly that descriptor.
//
// When apiVersion is empty, discovery resolves the preferred version.
func Resolve(ctx context.Context, cfg config.NamespacedRESTConfig, apiVersion string) (resources.Descriptor, error) {
	reg, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return resources.Descriptor{}, fmt.Errorf("discovery failed: %w", err)
	}

	selector := resources.Selector{
		Type:             resources.FilterTypeAll,
		GroupVersionKind: resources.PartialGVK{Resource: "dashboards"},
	}
	if apiVersion != "" {
		// Parse group/version (e.g. "dashboard.grafana.app/v1" or "v1").
		gv, parseErr := schema.ParseGroupVersion(apiVersion)
		if parseErr != nil {
			return resources.Descriptor{}, fmt.Errorf("invalid --api-version %q: %w", apiVersion, parseErr)
		}
		if gv.Version == "" {
			return resources.Descriptor{}, fmt.Errorf("invalid --api-version %q: version must not be empty", apiVersion)
		}
		selector.GroupVersionKind.Group = gv.Group
		selector.GroupVersionKind.Version = gv.Version
	}

	filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{
		Selectors:            resources.Selectors{selector},
		PreferredVersionOnly: true,
	})
	if err != nil {
		if apiVersion == "" {
			return resources.Descriptor{}, fmt.Errorf("no api-version specified, server does not expose the dashboards resource: %w", err)
		}
		return resources.Descriptor{}, fmt.Errorf("server does not support dashboards resource (api-version: %q): %w", apiVersion, err)
	}

	if len(filters) == 0 {
		if apiVersion == "" {
			return resources.Descriptor{}, errors.New("no api-version specified, server does not expose the dashboards resource")
		}
		return resources.Descriptor{}, fmt.Errorf("server does not support dashboards resource (api-version: %q)", apiVersion)
	}

	return filters[0].Descriptor, nil
}
