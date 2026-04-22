package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

// Client is an HTTP client for AI Observability agent catalog endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new agent client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns all agents.
func (c *Client) List(ctx context.Context) ([]Agent, error) {
	return aio11yhttp.ListAll[Agent](ctx, c.base, "/query/agents", nil)
}

// Lookup returns a single agent by name, optionally at a specific version.
func (c *Client) Lookup(ctx context.Context, name, version string) (*AgentDetail, error) {
	query := url.Values{"name": {name}}
	if version != "" {
		query.Set("version", version)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/query/agents/lookup?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup agent %s: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var detail AgentDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("failed to decode agent response: %w", err)
	}
	return &detail, nil
}

// Versions returns the version history for an agent by name.
func (c *Client) Versions(ctx context.Context, name string) ([]AgentVersion, error) {
	query := url.Values{"name": {name}}
	return aio11yhttp.ListAll[AgentVersion](ctx, c.base, "/query/agents/versions", query)
}
