package remote

import (
	"context"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/dynamic"
)

// NewDefaultClient exposes the resource tier's discovery and routing to
// product commands that need a focused resource operation.
func NewDefaultClient(ctx context.Context, cfg config.NamespacedRESTConfig) (*adapter.ResourceClientRouter, *discovery.Registry, error) {
	registry, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	client, err := dynamic.NewDefaultNamespacedClient(cfg)
	if err != nil {
		return nil, nil, err
	}
	return buildRouter(client, registry), registry, nil
}
