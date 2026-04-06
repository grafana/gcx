package fleet

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
)

// ConfigLoader can load Grafana Cloud configuration from the active context.
// This mirrors the interface in internal/providers/fleet/ to avoid a circular import.
type ConfigLoader interface {
	LoadCloudConfig(ctx context.Context) (providers.CloudRESTConfig, error)
}

// ClientResult holds the results of LoadClient including the fleet base client,
// resolved namespace, and the full stack info for deriving backend URLs and
// prom headers.
type ClientResult struct {
	Client    *Client
	Namespace string
	Stack     cloud.StackInfo
}

// LoadClient loads cloud configuration and constructs a Fleet Management client
// using Basic auth ({instanceID}:{apiToken}).
// Returns the client, the resolved namespace, and any error.
// Fails with a descriptive error if AgentManagementInstanceURL or
// AgentManagementInstanceID is not available for the stack.
func LoadClient(ctx context.Context, loader ConfigLoader) (*Client, string, error) {
	r, err := LoadClientWithStack(ctx, loader)
	if err != nil {
		return nil, "", err
	}
	return r.Client, r.Namespace, nil
}

// LoadClientWithStack is like LoadClient but also returns the full stack info,
// needed by instrumentation commands to derive backend URLs and prom headers.
func LoadClientWithStack(ctx context.Context, loader ConfigLoader) (*ClientResult, error) {
	cloudCfg, err := loader.LoadCloudConfig(ctx)
	if err != nil {
		return nil, err
	}

	url := cloudCfg.Stack.AgentManagementInstanceURL
	if url == "" {
		return nil, errors.New("fleet management endpoint is not available for this stack")
	}
	if cloudCfg.Stack.AgentManagementInstanceID == 0 {
		return nil, errors.New("fleet management instance ID is not available for this stack")
	}

	instanceID := strconv.Itoa(cloudCfg.Stack.AgentManagementInstanceID)
	httpClient, err := cloudCfg.HTTPClient()
	if err != nil {
		return nil, fmt.Errorf("fleet: failed to create HTTP client: %w", err)
	}

	return &ClientResult{
		Client:    NewClient(url, instanceID, cloudCfg.Token, true, httpClient),
		Namespace: cloudCfg.Namespace,
		Stack:     cloudCfg.Stack,
	}, nil
}
