package preferences

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// APIVersion is the API version for OrgPreferences resources.
	APIVersion = "preferences.ext.grafana.app/v1alpha1"
	// Kind is the kind for OrgPreferences resources.
	Kind = "OrgPreferences"
)

// StaticDescriptor returns the resource descriptor for organization preferences.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "preferences.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     Kind,
		Singular: "preferences",
		Plural:   "preferences",
	}
}

// PreferencesSchema returns a JSON Schema for the OrgPreferences resource type.
func PreferencesSchema() json.RawMessage {
	return adapter.SchemaFromType[OrgPreferences](StaticDescriptor())
}

// PreferencesExample returns an example OrgPreferences manifest as JSON.
func PreferencesExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "default",
		},
		"spec": map[string]any{
			"theme":     "dark",
			"timezone":  "UTC",
			"weekStart": "monday",
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("preferences: failed to marshal example: %v", err))
	}
	return b
}

// NewTypedCRUD creates a TypedCRUD for organization preferences.
// The provided loader resolves the active Grafana REST config.
func NewTypedCRUD(ctx context.Context, loader GrafanaConfigLoader) (*adapter.TypedCRUD[OrgPreferences], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for organization preferences: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create preferences HTTP client: %w", err)
	}

	crud := &adapter.TypedCRUD[OrgPreferences]{
		GetFn: func(ctx context.Context, _ string) (*OrgPreferences, error) {
			return client.Get(ctx)
		},
		UpdateFn: func(ctx context.Context, _ string, p *OrgPreferences) (*OrgPreferences, error) {
			if err := client.Update(ctx, p); err != nil {
				return nil, fmt.Errorf("failed to update organization preferences: %w", err)
			}
			updated, err := client.Get(ctx)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch updated organization preferences: %w", err)
			}
			return updated, nil
		},
		Namespace:  cfg.Namespace,
		Descriptor: StaticDescriptor(),
	}

	return crud, cfg, nil
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily from
// the default config file when invoked.
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		loader.SetContextName(internalconfig.ContextNameFromCtx(ctx))

		crud, _, err := NewTypedCRUD(ctx, &loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewFactoryFromConfig returns an adapter.Factory for organization preferences
// that creates a client using the provided NamespacedRESTConfig.
// The factory is lazy — the client is only created when the factory is invoked.
func NewFactoryFromConfig(cfg internalconfig.NamespacedRESTConfig) adapter.Factory {
	return func(_ context.Context) (adapter.ResourceAdapter, error) {
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create preferences HTTP client: %w", err)
		}

		crud := &adapter.TypedCRUD[OrgPreferences]{
			GetFn: func(ctx context.Context, _ string) (*OrgPreferences, error) {
				return client.Get(ctx)
			},
			UpdateFn: func(ctx context.Context, _ string, p *OrgPreferences) (*OrgPreferences, error) {
				if err := client.Update(ctx, p); err != nil {
					return nil, fmt.Errorf("failed to update organization preferences: %w", err)
				}
				updated, err := client.Get(ctx)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch updated organization preferences: %w", err)
				}
				return updated, nil
			},
			Namespace:  cfg.Namespace,
			Descriptor: StaticDescriptor(),
		}

		return crud.AsAdapter(), nil
	}
}

// ToResource converts an OrgPreferences to a gcx Resource, wrapping the fields
// in a Kubernetes-style object envelope with apiVersion, kind, and metadata.
func ToResource(p OrgPreferences, namespace string) (*resources.Resource, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OrgPreferences: %w", err)
	}

	var specMap map[string]any
	if err := json.Unmarshal(data, &specMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OrgPreferences to map: %w", err)
	}

	obj := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name":      p.GetResourceName(),
			"namespace": namespace,
		},
		"spec": specMap,
	}

	return resources.MustFromObject(obj, resources.SourceInfo{}), nil
}

// FromResource converts a gcx Resource back to an OrgPreferences.
func FromResource(res *resources.Resource) (*OrgPreferences, error) {
	obj := res.Object.Object

	specRaw, ok := obj["spec"]
	if !ok {
		return nil, errors.New("resource has no spec field")
	}

	specMap, ok := specRaw.(map[string]any)
	if !ok {
		return nil, errors.New("resource spec is not a map")
	}

	data, err := json.Marshal(specMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal spec: %w", err)
	}

	var p OrgPreferences
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spec to OrgPreferences: %w", err)
	}

	return &p, nil
}
