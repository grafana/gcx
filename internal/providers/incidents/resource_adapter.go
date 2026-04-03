package incidents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() { //nolint:gochecknoinits // Natural key registration for cross-stack push identity matching.
	adapter.RegisterNaturalKey(
		staticDescriptor.GroupVersionKind(),
		adapter.SpecFieldKey("title"),
	)
}

// staticDescriptor is the resource descriptor for incident resources.
//
//nolint:gochecknoglobals // Static descriptor used in init() self-registration pattern.
var staticDescriptor = resources.Descriptor{
	GroupVersion: schema.GroupVersion{
		Group:   "incident.ext.grafana.app",
		Version: "v1alpha1",
	},
	Kind:     "Incident",
	Singular: "incident",
	Plural:   "incidents",
}

// incidentSchema returns a JSON Schema for the Incident resource type.
func incidentSchema() json.RawMessage {
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"$id":     "https://grafana.com/schemas/Incident",
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
					"title":        map[string]any{"type": "string"},
					"status":       map[string]any{"type": "string"},
					"severity":     map[string]any{"type": "string"},
					"severityID":   map[string]any{"type": "string"},
					"isDrill":      map[string]any{"type": "boolean"},
					"incidentType": map[string]any{"type": "string"},
					"description":  map[string]any{"type": "string"},
					"labels":       map[string]any{"type": "array"},
				},
				"required": []string{"title", "status"},
			},
		},
		"required": []string{"apiVersion", "kind", "metadata", "spec"},
	}
	b, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Sprintf("incidents: failed to marshal schema: %v", err))
	}
	return b
}

// incidentExample returns an example Incident manifest as JSON.
func incidentExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": APIVersion,
		"kind":       Kind,
		"metadata": map[string]any{
			"name": "my-incident",
		},
		"spec": map[string]any{
			"title":        "Service degradation in production",
			"status":       "active",
			"isDrill":      false,
			"incidentType": "internal",
			"labels": []map[string]any{
				{"key": "team", "label": "platform"},
				{"key": "env", "label": "production"},
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("incidents: failed to marshal example: %v", err))
	}
	return b
}

// GrafanaConfigLoader can load a NamespacedRESTConfig from the active context.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
}

// NewAdapterFactory returns a lazy adapter.Factory for incidents.
// The factory captures the GrafanaConfigLoader and constructs the client on first invocation.
func NewAdapterFactory(loader GrafanaConfigLoader) adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for incidents adapter: %w", err)
		}

		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create incidents client: %w", err)
		}

		return newTypedAdapter(client, cfg.Namespace), nil
	}
}

// NewFactoryFromConfig returns an adapter.Factory for incidents that
// creates a Client using the provided NamespacedRESTConfig.
func NewFactoryFromConfig(cfg internalconfig.NamespacedRESTConfig) adapter.Factory {
	return func(_ context.Context) (adapter.ResourceAdapter, error) {
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create incidents client: %w", err)
		}

		return newTypedAdapter(client, cfg.Namespace), nil
	}
}

// newTypedAdapter builds the TypedCRUD[Incident] adapter for the given client and namespace.
func newTypedAdapter(client *Client, namespace string) adapter.ResourceAdapter {
	crud := &adapter.TypedCRUD[Incident]{
		ListFn: func(ctx context.Context) ([]Incident, error) {
			return client.List(ctx, IncidentQuery{})
		},

		GetFn: func(ctx context.Context, name string) (*Incident, error) {
			return client.Get(ctx, name)
		},

		CreateFn: func(ctx context.Context, inc *Incident) (*Incident, error) {
			return client.Create(ctx, inc)
		},

		UpdateFn: func(ctx context.Context, name string, inc *Incident) (*Incident, error) {
			return client.UpdateStatus(ctx, name, inc.Status)
		},

		DeleteFn: func(_ context.Context, _ string) error {
			return errors.New("incidents: delete is not supported by the IRM API")
		},

		StripFields: []string{"incidentID"},
		Namespace:   namespace,
		Descriptor:  staticDescriptor,
	}

	return crud.AsAdapter()
}

// NewTypedCRUD creates a TypedCRUD for incidents.
// The query parameter controls listing behaviour (limit, ordering, etc.).
func NewTypedCRUD(ctx context.Context, loader GrafanaConfigLoader, query IncidentQuery) (*adapter.TypedCRUD[Incident], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for incidents: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create incidents client: %w", err)
	}

	crud := &adapter.TypedCRUD[Incident]{
		ListFn: func(ctx context.Context) ([]Incident, error) {
			return client.List(ctx, query)
		},

		GetFn: func(ctx context.Context, name string) (*Incident, error) {
			return client.Get(ctx, name)
		},

		CreateFn: func(ctx context.Context, inc *Incident) (*Incident, error) {
			return client.Create(ctx, inc)
		},

		UpdateFn: func(ctx context.Context, name string, inc *Incident) (*Incident, error) {
			return client.UpdateStatus(ctx, name, inc.Status)
		},

		DeleteFn: func(_ context.Context, _ string) error {
			return errors.New("incidents: delete is not supported by the IRM API")
		},

		StripFields: []string{"incidentID"},
		Namespace:   cfg.Namespace,
		Descriptor:  staticDescriptor,
	}

	return crud, cfg, nil
}
