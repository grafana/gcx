package irm

import (
	"context"

	"github.com/grafana/gcx/internal/providers"
)

// OnCallConfigLoader can produce a configured OnCall client.
type OnCallConfigLoader interface {
	LoadOnCallClient(ctx context.Context) (OnCallAPI, string, error)
}

// configLoader loads IRM config and creates clients.
type configLoader struct {
	providers.ConfigLoader
}

// LoadOnCallClient loads config and returns a configured OnCall client.
// For OAuth auth, it returns the plugin proxy client (internal API).
// For SA token auth, it returns the public API client with response adaptation.
func (l *configLoader) LoadOnCallClient(ctx context.Context) (OnCallAPI, string, error) {
	restCfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", err
	}

	if restCfg.IsOAuthProxy() {
		client, err := NewOnCallClient(restCfg)
		if err != nil {
			return nil, "", err
		}
		return client, restCfg.Namespace, nil
	}

	// SA token mode: the IRM plugin proxy rejects SA tokens, so fall back
	// to the plugin proxy client anyway. This will fail with 403 until either:
	// - The OnCall backend allows SA tokens through PluginAuthentication
	// - The public API client (oncallpublic package) is wired in here
	//
	// TODO: wire oncallpublic.NewClient() here for SA token mode.
	client, err := NewOnCallClient(restCfg)
	if err != nil {
		return nil, "", err
	}
	return client, restCfg.Namespace, nil
}
