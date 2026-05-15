package vulnobs

import (
	"context"
	"encoding/json"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// APIVersion is the GVK group/version for vulnobs typed resources.
	APIVersion = "vulnobs.grafana.app/v1alpha1"
	// SourceKind is the Kind for `Source` (repository) resources.
	SourceKind = "Source"
)

//nolint:gochecknoglobals // Static descriptors used in init() self-registration pattern.
var (
	vulnobsGroupVersion = schema.GroupVersion{Group: "vulnobs.grafana.app", Version: "v1alpha1"}

	sourceDescriptor = resources.Descriptor{
		GroupVersion: vulnobsGroupVersion,
		Kind:         SourceKind,
		Singular:     "source",
		Plural:       "sources",
	}
)

// SourceDescriptor exposes the Source descriptor for tests and registration.
func SourceDescriptor() resources.Descriptor { return sourceDescriptor }

// SourceSchema returns a JSON Schema for the Source resource. Example is
// nil per CONSTITUTION line 45 because Source is read-only (no Create/Update).
func SourceSchema() json.RawMessage {
	specProps := map[string]any{
		"name":       map[string]any{"type": "string", "description": "Canonical source name, e.g. owner/repo."},
		"type":       map[string]any{"type": "string", "description": `Source type. Always "repository" for current Grafana usage.`},
		"origin":     map[string]any{"type": "string", "description": `Upstream origin. Typically "github".`},
		"visibility": map[string]any{"type": "string", "enum": []string{"public", "private"}},
		"integration": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":   map[string]any{"type": "integer"},
				"name": map[string]any{"type": "string"},
				"type": map[string]any{"type": "string"},
			},
		},
		"groups": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":   map[string]any{"type": "integer"},
					"name": map[string]any{"type": "string"},
				},
			},
		},
		"versions": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":                 map[string]any{"type": "integer"},
					"tag":                map[string]any{"type": "string"},
					"publishDate":        map[string]any{"type": "string"},
					"lowestSloRemaining": map[string]any{"type": "integer"},
					"totalCveCounts": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"critical": map[string]any{"type": "integer"},
							"high":     map[string]any{"type": "integer"},
							"medium":   map[string]any{"type": "integer"},
							"low":      map[string]any{"type": "integer"},
						},
					},
				},
			},
		},
	}
	s := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/VulnobsSource",
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": APIVersion},
			"kind":       map[string]any{"type": "string", "const": SourceKind},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": map[string]any{
				"type":       "object",
				"properties": specProps,
				"required":   []string{"name"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("vulnobs: failed to marshal Source schema: %v", err))
	}
	return b
}

// RESTConfigLoader can load a NamespacedRESTConfig from the active context.
type RESTConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
}

// NewSourceAdapterFactory returns a lazy adapter.Factory for vulnobs Sources.
// The adapter is read-only: List is satisfied via the Projects query, Get
// falls back to list-then-filter (TypedCRUD default), Create/Update/Delete
// are unset and return adapter's standard "not supported" errors.
func NewSourceAdapterFactory(loader RESTConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("vulnobs: failed to load REST config: %w", err)
		}
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("vulnobs: failed to create client: %w", err)
		}
		crud := &adapter.TypedCRUD[Source]{
			ListFn: adapter.LimitedListFn(func(ctx context.Context) ([]Source, error) {
				sources, _, err := client.Projects(ctx, ProjectsOptions{
					ShowArchived: true,
					First:        1000,
				})
				return sources, err
			}),
			Namespace:  cfg.Namespace,
			Descriptor: sourceDescriptor,
		}
		return crud.AsAdapter(), nil
	}
}

// SourceToResource converts a Source to a gcx Resource for codec output.
func SourceToResource(src Source, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(src)
	if err != nil {
		return nil, fmt.Errorf("vulnobs: marshal Source: %w", err)
	}
	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("vulnobs: unmarshal Source: %w", err)
	}
	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       SourceKind,
		"metadata": map[string]any{
			"name":      src.GetResourceName(),
			"namespace": namespace,
		},
		"spec": specMap,
	}
	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}
