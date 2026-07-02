package datasources

import (
	"context"

	"github.com/grafana/gcx/internal/config"
)

// Transport is the datasource lifecycle interface used by the commands. It is
// implemented by the legacy REST Client, the app-platform k8sTransport, and the
// dualTransport that prefers the latter and falls back to the former per call.
type Transport interface {
	List(ctx context.Context) ([]*Datasource, error)
	GetByUID(ctx context.Context, uid string) (*Datasource, error)
	Create(ctx context.Context, ds *Datasource) (*Datasource, error)
	Update(ctx context.Context, uid string, ds *Datasource) (*Datasource, error)
	Delete(ctx context.Context, uid string) error
	Health(ctx context.Context, uid string) (*HealthResult, error)
}

var _ Transport = (*Client)(nil)

// NewTransport returns the dual-mode datasource transport: it prefers the
// Grafana app-platform API (/apis/{pluginID}.datasource.grafana.app/...) when
// served and transparently falls back to the legacy /api/datasources REST API.
// Neither client performs I/O at construction.
func NewTransport(cfg config.NamespacedRESTConfig) (Transport, error) {
	restClient, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	k8sClient, err := newK8sTransport(cfg)
	if err != nil {
		return nil, err
	}
	return &dualTransport{rest: restClient, k8s: k8sClient}, nil
}
