package definitions

import (
	"context"
	"encoding/json"
	"fmt"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func init() { //nolint:gochecknoinits // Natural key registration for cross-stack push identity matching.
	adapter.RegisterNaturalKey(
		StaticDescriptor().GroupVersionKind(),
		adapter.SpecFieldKey("name"),
	)
}

// StaticDescriptor returns the resource descriptor for SLO definitions.
func StaticDescriptor() resources.Descriptor {
	return resources.Descriptor{
		GroupVersion: schema.GroupVersion{
			Group:   "slo.ext.grafana.app",
			Version: "v1alpha1",
		},
		Kind:     "SLO",
		Singular: "slo",
		Plural:   "slos",
	}
}

// SloSchema returns a JSON Schema for the SLO resource type.
func SloSchema() json.RawMessage {
	return adapter.SchemaFromType[Slo](StaticDescriptor())
}

// SloExample returns an example SLO manifest as JSON.
func SloExample() json.RawMessage {
	example := map[string]any{
		"apiVersion": "slo.ext.grafana.app/v1alpha1",
		"kind":       "SLO",
		"metadata": map[string]any{
			"name": "my-slo",
		},
		"spec": map[string]any{
			"name":        "HTTP Availability",
			"description": "Tracks HTTP request success rate",
			"query": map[string]any{
				"type": "freeform",
				"freeform": map[string]any{
					"query": `sum(rate(http_requests_total{status!~"5.."}[5m])) / sum(rate(http_requests_total[5m]))`,
				},
			},
			"objectives": []map[string]any{
				{"value": 0.995, "window": "28d"},
			},
			"labels": []map[string]any{
				{"key": "team", "value": "platform"},
			},
		},
	}
	b, err := json.Marshal(example)
	if err != nil {
		panic(fmt.Sprintf("slo/definitions: failed to marshal example: %v", err))
	}
	return b
}

// NewTypedCRUD creates a TypedCRUD for SLO definitions using the provided loader.
// Returns both the CRUD instance and the config for additional operations like Prometheus queries.
func NewTypedCRUD(ctx context.Context, loader GrafanaConfigLoader) (*adapter.TypedCRUD[Slo], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to load REST config for SLO: %w", err)
	}

	client, err := NewClient(cfg)
	if err != nil {
		return nil, internalconfig.NamespacedRESTConfig{}, fmt.Errorf("failed to create SLO definitions client: %w", err)
	}

	crud := &adapter.TypedCRUD[Slo]{
		ListFn: adapter.LimitedListFn(client.List),
		GetFn: func(ctx context.Context, name string) (*Slo, error) {
			return client.Get(ctx, name)
		},
		CreateFn: func(ctx context.Context, slo *Slo) (*Slo, error) {
			return client.Create(ctx, slo)
		},
		UpdateFn: func(ctx context.Context, name string, slo *Slo) (*Slo, error) {
			return client.Update(ctx, name, slo)
		},
		DeleteFn: func(ctx context.Context, name string) error {
			// DeleteFn is only reached after the CLI layer (definitions'
			// own delete command or the generic `resources delete`) has
			// already run its own confirm/force check, so confirmation is
			// always satisfied by the time the typed client sees it.
			return client.Delete(ctx, name, true)
		},
		Namespace:   cfg.Namespace,
		StripFields: []string{"uuid", "readOnly"},
		Descriptor:  StaticDescriptor(),
	}
	return crud, cfg, nil
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily from the
// default config file when invoked. This is used for global adapter registration in init()
// and by SLOProvider.ResourceAdapters().
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		crud, _, err := NewTypedCRUD(ctx, &loader)
		if err != nil {
			return nil, err
		}
		return crud.AsAdapter(), nil
	}
}

// NewFactoryFromConfig returns an adapter.Factory for SLO definitions that
// creates a definitions.Client using the provided NamespacedRESTConfig.
// The factory is lazy — the client is only created when the factory is invoked.
func NewFactoryFromConfig(cfg internalconfig.NamespacedRESTConfig) adapter.Factory {
	return func(_ context.Context) (adapter.ResourceAdapter, error) {
		client, err := NewClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create SLO definitions client: %w", err)
		}

		crud := &adapter.TypedCRUD[Slo]{
			ListFn: adapter.LimitedListFn(client.List),
			GetFn: func(ctx context.Context, name string) (*Slo, error) {
				return client.Get(ctx, name)
			},
			CreateFn: func(ctx context.Context, slo *Slo) (*Slo, error) {
				return client.Create(ctx, slo)
			},
			UpdateFn: func(ctx context.Context, name string, slo *Slo) (*Slo, error) {
				return client.Update(ctx, name, slo)
			},
			DeleteFn: func(ctx context.Context, name string) error {
				// DeleteFn is only reached after the CLI layer (definitions'
				// own delete command or the generic `resources delete`) has
				// already run its own confirm/force check, so confirmation is
				// always satisfied by the time the typed client sees it.
				return client.Delete(ctx, name, true)
			},
			Namespace:   cfg.Namespace,
			StripFields: []string{"uuid", "readOnly"},
			Descriptor:  StaticDescriptor(),
		}
		return crud.AsAdapter(), nil
	}
}
