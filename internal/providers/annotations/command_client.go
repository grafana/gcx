package annotations

import (
	"context"
	"fmt"
	"strconv"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// RESTConfigLoader loads the active Grafana configuration.
type RESTConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
}

// commandResource defines typed command output. It has no resource registration.
func commandResource() adapter.Resource[Annotation] {
	return adapter.Resource[Annotation]{Group: "annotations.ext.grafana.app", Version: "v1alpha1", Kind: "Annotation"}
}

type commandClient struct {
	client      *Client
	listOptions ListOptions
}

func (c commandClient) List(ctx context.Context, _ adapter.ListOptions) ([]Annotation, error) {
	return c.client.List(ctx, c.listOptions)
}

func (c commandClient) Get(ctx context.Context, name string) (*Annotation, error) {
	id, err := strconv.ParseInt(name, 10, 64)
	if err != nil {
		return nil, err
	}
	return c.client.Get(ctx, id)
}

func (c commandClient) Create(ctx context.Context, a *Annotation) (*Annotation, error) {
	a.ID = 0
	if err := c.client.Create(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (c commandClient) Delete(ctx context.Context, name string) error {
	id, err := strconv.ParseInt(name, 10, 64)
	if err != nil {
		return err
	}
	return c.client.Delete(ctx, id)
}

func newCommandCRUD(ctx context.Context, loader RESTConfigLoader, opts ListOptions) (*adapter.TypedCRUD[Annotation], internalconfig.NamespacedRESTConfig, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, cfg, fmt.Errorf("failed to load annotations config: %w", err)
	}
	client, err := NewClient(cfg)
	if err != nil {
		return nil, cfg, err
	}
	return commandResource().TypedCRUD(commandClient{client: client, listOptions: opts}, cfg.Namespace), cfg, nil
}
