package fleet

import (
	"context"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
)

// ConfigLoader resolves the Grafana REST config for the active context. Fleet
// and instrumentation reach the Fleet Management API through the
// grafana-collector-app plugin proxy at cfg.Host, so they authenticate with the
// active context's Grafana credential — no Cloud access-policy token is
// required.
//
// This declares the subset of internal/providers.ConfigLoader that the fleet
// packages need, kept here to avoid a circular import.
type ConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// ClientResult holds the results of LoadClientWithStack: the fleet base client,
// the resolved namespace, and the stack instance metadata used by
// instrumentation to derive backend datasource URLs.
type ClientResult struct {
	Client    *Client
	Namespace string
	Stack     cloud.StackInfo
}

// LoadClient resolves the Grafana REST config for the active context and
// constructs a Fleet Management base client routed through the collector-app
// plugin proxy. Returns the client and the resolved namespace. No Cloud
// access-policy token or Fleet Management instance URL/ID is consulted.
func LoadClient(ctx context.Context, loader ConfigLoader) (*Client, string, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return nil, "", err
	}
	return client, cfg.Namespace, nil
}

// LoadClientWithStack is like LoadClient but also fetches the stack instance
// metadata through the collector-app's Viewer-role instance-metadata proxy
// route. Instrumentation mutations use the returned Stack to build backend
// datasource URLs; reads that do not need it should call LoadClient instead.
func LoadClientWithStack(ctx context.Context, loader ConfigLoader) (*ClientResult, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return nil, err
	}
	stack, err := client.FetchInstanceMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &ClientResult{
		Client:    client,
		Namespace: cfg.Namespace,
		Stack:     stack,
	}, nil
}
