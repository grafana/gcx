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

// BoundResource binds a declaration to a configuration loader without doing I/O.
// Each Load resolves fresh configuration; clients are never cached across executions.
type BoundResource[T adapter.ResourceNamer] struct {
	loader   GrafanaConfigLoader
	resource adapter.Resource[T]
}

// BindGrafanaResource declares a command group's resource dependency.
func BindGrafanaResource[T adapter.ResourceNamer](loader GrafanaConfigLoader, resource adapter.Resource[T]) BoundResource[T] {
	return BoundResource[T]{loader: loader, resource: resource}
}

// Load constructs typed CRUD and returns the same configuration snapshot for
// auxiliary queries. Call it after validation and any destructive confirmation.
func (b BoundResource[T]) Load(ctx context.Context) (*adapter.TypedCRUD[T], config.NamespacedRESTConfig, error) {
	deps, cfg, err := loadGrafanaDeps(ctx, b.loader)
	if err != nil {
		return nil, cfg, err
	}
	client, err := b.resource.NewClient(ctx, deps)
	if err != nil {
		return nil, cfg, fmt.Errorf("failed to construct %s client: %w", b.resource.Kind, err)
	}
	return b.resource.TypedCRUD(client, deps.Namespace), cfg, nil
}
