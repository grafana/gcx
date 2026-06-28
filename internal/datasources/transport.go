package datasources

import (
	"context"

	"github.com/grafana/gcx/internal/config"
)

// Transport is the datasource lifecycle interface used by the commands. It is
// implemented today by the legacy REST Client; a Kubernetes app-platform
// implementation can be added behind the same interface when those APIs are
// served, without touching the command layer.
type Transport interface {
	List(ctx context.Context) ([]*Datasource, error)
	GetByUID(ctx context.Context, uid string) (*Datasource, error)
	Create(ctx context.Context, ds *Datasource) (*Datasource, error)
	Update(ctx context.Context, uid string, ds *Datasource) (*Datasource, error)
	Delete(ctx context.Context, uid string) error
	Health(ctx context.Context, uid string) (*HealthResult, error)
}

var _ Transport = (*Client)(nil)

// NewTransport returns the datasource transport for the given config. It
// currently returns the legacy REST client.
func NewTransport(cfg config.NamespacedRESTConfig) (Transport, error) {
	return NewClient(cfg)
}
