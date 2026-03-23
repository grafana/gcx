package definitions

import (
	"context"
	"fmt"

	internalconfig "github.com/grafana/grafanactl/internal/config"
	"github.com/grafana/grafanactl/internal/providers"
	"github.com/grafana/grafanactl/internal/resources"
	"github.com/grafana/grafanactl/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

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

// StaticAliases returns the short aliases for SLO resources.
func StaticAliases() []string {
	return []string{"slo"}
}

func init() { //nolint:gochecknoinits // Self-registration pattern (like database/sql drivers).
	desc := StaticDescriptor()
	adapter.Register(adapter.Registration{
		Factory:    NewLazyFactory(),
		Descriptor: desc,
		Aliases:    StaticAliases(),
		GVK:        desc.GroupVersionKind(),
	})
}

// NewLazyFactory returns an adapter.Factory that loads its config lazily from the
// default config file when invoked. This is used for global adapter registration in init()
// and by SLOProvider.ResourceAdapters().
func NewLazyFactory() adapter.Factory {
	return func(ctx context.Context) (adapter.ResourceAdapter, error) {
		var loader providers.ConfigLoader
		loader.SetContextName(internalconfig.ContextNameFromCtx(ctx))

		cfg, err := loader.LoadGrafanaConfig(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load REST config for SLO adapter: %w", err)
		}

		return NewFactoryFromConfig(cfg)(ctx)
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
			NameFn: func(s Slo) string { return s.UUID },
			ListFn: client.List,
			GetFn: func(ctx context.Context, name string) (*Slo, error) {
				return client.Get(ctx, name)
			},
			CreateFn: func(ctx context.Context, slo *Slo) (*Slo, error) {
				resp, err := client.Create(ctx, slo)
				if err != nil {
					return nil, fmt.Errorf("failed to create SLO: %w", err)
				}
				created, err := client.Get(ctx, resp.UUID)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch created SLO %q: %w", resp.UUID, err)
				}
				return created, nil
			},
			UpdateFn: func(ctx context.Context, name string, slo *Slo) (*Slo, error) {
				if err := client.Update(ctx, name, slo); err != nil {
					return nil, fmt.Errorf("failed to update SLO %q: %w", name, err)
				}
				updated, err := client.Get(ctx, name)
				if err != nil {
					return nil, fmt.Errorf("failed to fetch updated SLO %q: %w", name, err)
				}
				return updated, nil
			},
			DeleteFn: func(ctx context.Context, name string) error {
				return client.Delete(ctx, name)
			},
			Namespace:     cfg.Namespace,
			StripFields:   []string{"uuid", "readOnly"},
			RestoreNameFn: func(name string, slo *Slo) { slo.UUID = name },
			Descriptor:    StaticDescriptor(),
			Aliases:       StaticAliases(),
		}
		return crud.AsAdapter(), nil
	}
}
