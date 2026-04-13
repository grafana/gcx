package irm

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/irm/oncallpublic"
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

	// SA token mode: the IRM plugin proxy rejects SA tokens, so call the
	// OnCall public API directly. This path will be removed when the OnCall
	// backend supports SA tokens through PluginAuthentication.
	oncallURL, err := l.discoverOnCallURL(ctx)
	if err != nil {
		return nil, "", err
	}
	client, err := oncallpublic.NewClient(ctx, oncallURL, restCfg)
	if err != nil {
		return nil, "", err
	}
	return client, restCfg.Namespace, nil
}

// discoverOnCallURL resolves the OnCall API URL from provider config or plugin settings.
func (l *configLoader) discoverOnCallURL(ctx context.Context) (string, error) {
	providerCfg, _, err := l.LoadProviderConfig(ctx, "oncall")
	if err != nil {
		return "", err
	}
	if u := providerCfg["oncall-url"]; u != "" {
		return u, nil
	}

	cfg, err := l.LoadGrafanaConfig(ctx)
	if err != nil {
		return "", err
	}
	discovered, err := oncallpublic.DiscoverOnCallURL(ctx, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to discover OnCall API URL: %w", err)
	}
	return discovered, nil
}
