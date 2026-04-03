package faro

import (
	"context"
	"encoding/json"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() { //nolint:gochecknoinits // Natural key registration for cross-stack push identity matching.
	adapter.RegisterNaturalKey(
		staticDescriptor.GroupVersionKind(),
		adapter.SpecFieldKey("name"),
	)
}

const (
	// APIVersion is the API version for Faro app resources.
	APIVersion = "faro.ext.grafana.app/v1alpha1"
	// Kind is the kind for Faro app resources.
	Kind = "FaroApp"
)

// staticDescriptor is the resource descriptor for Faro app resources.
//
//nolint:gochecknoglobals // Static descriptor used in self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "faro.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "FaroApp",
	Singular: "app",
	Plural:   "apps",
}

// FaroAppSchema returns a JSON Schema for the FaroApp resource type.
func FaroAppSchema() json.RawMessage {
	s := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/FaroApp",
		"type":    "object",
		"properties": map[string]any{
			"apiVersion": map[string]any{"type": "string", "const": APIVersion},
			"kind":       map[string]any{"type": "string", "const": Kind},
			"metadata": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":      map[string]any{"type": "string"},
					"namespace": map[string]any{"type": "string"},
				},
			},
			"spec": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name":               map[string]any{"type": "string"},
					"corsOrigins":        map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"url": map[string]any{"type": "string"}}}},
					"extraLogLabels":     map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					"collectEndpointURL": map[string]any{"type": "string"},
					"appKey":             map[string]any{"type": "string"},
					"settings": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"geolocationEnabled": map[string]any{"type": "boolean"},
							"geolocationLevel":   map[string]any{"type": "string", "enum": []string{"country", "region", "city"}},
						},
					},
				},
				"required": []string{"name"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("faro: failed to marshal schema: %v", err))
	}
	return b
}

// FaroAppExample returns an example FaroApp manifest as JSON.
func FaroAppExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "my-web-app-42",
		},
		"spec": map[string]any{
			"name": "my-web-app",
			"corsOrigins": []map[string]any{
				{"url": "https://app.example.com"},
				{"url": "https://staging.example.com"},
			},
			"extraLogLabels": map[string]string{
				"team": "frontend",
			},
			"settings": map[string]any{
				"geolocationEnabled": true,
				"geolocationLevel":   "country",
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("faro: failed to marshal example: %v", err))
	}
	return b
}

// RESTConfigLoader can load a NamespacedRESTConfig from the active context.
type RESTConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
}

// NewAdapterFactory returns a lazy adapter.Factory for Faro apps.
// The factory captures the RESTConfigLoader and constructs the client on first invocation.
func NewAdapterFactory(loader RESTConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for faro adapter: %w", err)
		}

		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create faro client: %w", err)
		}

		return newTypedAdapter(client, cfg.Namespace), nil
	}
}

// NewFactoryFromConfig returns an adapter.Factory for Faro apps that
// creates a Client using the provided NamespacedRESTConfig.
func NewFactoryFromConfig(cfg internalconfig.NamespacedRESTConfig) adapter.Factory {
	return func(_ context.Context) (adapter.ResourceAdapter, error) {
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create faro client: %w", err)
		}

		return newTypedAdapter(client, cfg.Namespace), nil
	}
}

// newTypedAdapter builds the TypedCRUD[FaroApp] adapter for the given client and namespace.
func newTypedAdapter(client *Client, namespace string) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[FaroApp]{
		ListFn: func(ctx context.Context) ([]FaroApp, error) {
			return client.List(ctx)
		},

		GetFn: func(ctx context.Context, name string) (*FaroApp, error) {
			id, ok := adapter.ExtractIDFromSlug(name)
			if !ok {
				// Try as bare name.
				id = name
			}
			return client.Get(ctx, id)
		},

		CreateFn: func(ctx context.Context, app *FaroApp) (*FaroApp, error) {
			return client.Create(ctx, app)
		},

		UpdateFn: func(ctx context.Context, name string, app *FaroApp) (*FaroApp, error) {
			id, ok := adapter.ExtractIDFromSlug(name)
			if !ok {
				id = name
			}
			return client.Update(ctx, id, app)
		},

		DeleteFn: func(ctx context.Context, name string) error {
			id, ok := adapter.ExtractIDFromSlug(name)
			if !ok {
				id = name
			}
			return client.Delete(ctx, id)
		},

		StripFields: []string{"id"},
		Namespace:   namespace,
		Descriptor:  staticDescriptor,
	}

	return crud.AsAdapter()
}
