package agents

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

const (
	agentsPath        = "/query/agents"
	agentLookupPath   = agentsPath + "/lookup"
	agentVersionsPath = agentsPath + "/versions"
)

// Client is an HTTP client for AI Observability agent catalog endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new agent client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns agents, limited to the given count. Pass 0 for no limit.
func (c *Client) List(ctx context.Context, limit int) ([]Agent, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	return aio11yhttp.ListAll[Agent](ctx, c.base, agentsPath, query, limit)
}

// Lookup returns a single agent by name, optionally at a specific version.
func (c *Client) Lookup(ctx context.Context, name, version string) (*AgentDetail, error) {
	query := url.Values{"name": {name}}
	if version != "" {
		query.Set("version", version)
	}

	detail, err := aio11yhttp.DoJSON[any, AgentDetail](ctx, c.base, http.MethodGet, agentLookupPath+"?"+query.Encode(), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

// Versions returns the version history for an agent by name.
func (c *Client) Versions(ctx context.Context, name string) ([]AgentVersion, error) {
	query := url.Values{"name": {name}}
	return aio11yhttp.ListAll[AgentVersion](ctx, c.base, agentVersionsPath, query)
}
