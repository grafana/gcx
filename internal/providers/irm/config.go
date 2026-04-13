package irm

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
)

// OnCallConfigLoader can produce a configured OnCall client.
type OnCallConfigLoader interface {
	LoadOnCallClient(ctx context.Context) (*OnCallClient, string, error)
}

// configLoader loads IRM config and creates clients.
type configLoader struct {
	providers.ConfigLoader
}

// LoadOnCallClient loads config and returns a configured OnCall client.
func (l *configLoader) LoadOnCallClient(ctx context.Context) (*OnCallClient, string, error) {
	restCfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", err
	}

	client, err := NewOnCallClient(restCfg)
	if err != nil {
		return nil, "", err
	}
	return client, restCfg.Namespace, nil
}
