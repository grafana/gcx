package providers

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/client-go/rest"
)

// GrafanaConfigLoader resolves the active Grafana connection for a command.
type GrafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// LoadGrafanaDeps supplies lazy resource factories with the active Grafana
// transport. Product-specific transports should continue using their own loader.
func LoadGrafanaDeps(ctx context.Context) (adapter.ClientDeps, error) {
	var loader ConfigLoader
	deps, _, err := loadGrafanaDeps(ctx, &loader)
	return deps, err
}

func loadGrafanaDeps(ctx context.Context, loader GrafanaConfigLoader) (adapter.ClientDeps, config.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return adapter.ClientDeps{}, cfg, fmt.Errorf("failed to load Grafana config: %w", err)
	}
	client, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return adapter.ClientDeps{}, cfg, fmt.Errorf("failed to create Grafana HTTP client: %w", err)
	}
	return adapter.ClientDeps{HTTP: client, BaseURL: cfg.Host, Namespace: cfg.Namespace}, cfg, nil
}

// LoadGrafanaResource constructs the command's typed CRUD from its registered
// declaration. It returns the same config snapshot for auxiliary queries, so
// resource operations and queries cannot resolve different stacks.
func LoadGrafanaResource[T adapter.ResourceNamer](ctx context.Context, loader GrafanaConfigLoader, resource adapter.Resource[T]) (*adapter.TypedCRUD[T], config.NamespacedRESTConfig, error) {
	deps, cfg, err := loadGrafanaDeps(ctx, loader)
	if err != nil {
		return nil, cfg, err
	}
	client, err := resource.NewClient(ctx, deps)
	if err != nil {
		return nil, cfg, fmt.Errorf("failed to construct %s client: %w", resource.Kind, err)
	}
	return resource.TypedCRUD(client, deps.Namespace), cfg, nil
}
