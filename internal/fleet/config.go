package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

const (
	// CollectorAppID is the Grafana app plugin that proxies Fleet Management.
	CollectorAppID = "grafana-collector-app"

	// CollectorAppReadAction grants access to the plugin's named read routes.
	CollectorAppReadAction = CollectorAppID + ":read"

	// CollectorAppAdminAction grants access to the plugin's wildcard routes.
	// Some read-only Fleet operations use these routes.
	CollectorAppAdminAction = CollectorAppID + ":admin"

	// pluginProxyPath is the plugin proxy prefix for the Fleet Management RPCs.
	pluginProxyPath = "/api/plugin-proxy/" + CollectorAppID + "/fleet-management-api"

	// stackInfoPath is the plugin proxy route that returns the grafana.com
	// instance record for the current stack. It needs the Viewer role only.
	stackInfoPath = "/api/plugin-proxy/" + CollectorAppID + "/grafanacom-api/instances/"
)

// ConfigLoader can load the Grafana stack configuration from the active context.
// This mirrors the interface in internal/providers/fleet/ to avoid a circular import.
type ConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (config.NamespacedRESTConfig, error)
}

// ClientResult holds the results of LoadClientWithStack including the fleet base
// client, resolved namespace, and the full stack info for deriving backend URLs
// and prom headers.
type ClientResult struct {
	Client    *Client
	Namespace string
	Stack     cloud.StackInfo
}

// LoadClient loads the Grafana stack configuration and constructs a Fleet
// Management client that talks to the collector app plugin proxy.
// Returns the client, the resolved namespace, and any error.
func LoadClient(ctx context.Context, loader ConfigLoader) (*Client, string, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, "", err
	}

	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, "", fmt.Errorf("fleet: failed to create HTTP client: %w", err)
	}

	return NewClient(ctx, cfg.Host+pluginProxyPath, httpClient), cfg.Namespace, nil
}

// LoadClientWithStack is like LoadClient but also returns the full stack info,
// needed by instrumentation commands to derive backend URLs and prom headers.
// The stack info comes from the collector app plugin proxy, so it needs no
// grafana.com token.
func LoadClientWithStack(ctx context.Context, loader ConfigLoader) (*ClientResult, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, err
	}

	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("fleet: failed to create HTTP client: %w", err)
	}

	stack, err := getStackInfo(ctx, cfg.Host, httpClient)
	if err != nil {
		return nil, err
	}

	return &ClientResult{
		Client:    NewClient(ctx, cfg.Host+pluginProxyPath, httpClient),
		Namespace: cfg.Namespace,
		Stack:     stack,
	}, nil
}

func getStackInfo(ctx context.Context, host string, httpClient *http.Client) (cloud.StackInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, host+stackInfoPath, nil)
	if err != nil {
		return cloud.StackInfo{}, fmt.Errorf("fleet: create stack info request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return cloud.StackInfo{}, fmt.Errorf("fleet: fetch stack info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return cloud.StackInfo{}, &HTTPError{
			Status: resp.StatusCode,
			Path:   stackInfoPath,
			Body:   ReadErrorBody(resp),
		}
	}

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBodyBytes)
	if err != nil {
		return cloud.StackInfo{}, fmt.Errorf("fleet: read stack info: %w", err)
	}

	var stack cloud.StackInfo
	if err := json.Unmarshal(body, &stack); err != nil {
		return cloud.StackInfo{}, fmt.Errorf("fleet: decode stack info: %w", err)
	}
	return stack, nil
}
