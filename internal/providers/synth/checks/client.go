package checks

import (
	"context"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/grafana/gcx/pkg/gfc/sm"
)

// Client bridges gcx's config system to the shared SM checks client.
type Client struct {
	*sm.ChecksClient
}

// NewClient creates a checks client over the shared dual-mode SM transport.
//
// When datasourceUID is non-empty, requests go through the Grafana datasource
// proxy built from restCfg (carrying the caller's Grafana credential). On a
// proxy 403 — or when datasourceUID is empty — requests fall back to the direct
// SM API, with credentials resolved lazily via fallback.LoadSMConfig. A nil
// fallback disables the direct path.
func NewClient(ctx context.Context, restCfg config.NamespacedRESTConfig, datasourceUID string, fallback smcfg.FallbackLoader) (*Client, error) {
	t, err := smcfg.NewSMTransport(ctx, restCfg, datasourceUID, fallback)
	if err != nil {
		return nil, err
	}
	return &Client{ChecksClient: &sm.ChecksClient{T: t}}, nil
}
