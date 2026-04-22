package generations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

// Client is an HTTP client for AI Observability generation endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new generations client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// Get returns a single generation by ID.
func (c *Client) Get(ctx context.Context, id string) (map[string]any, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/query/generations/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get generation %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var detail map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("failed to decode generation response: %w", err)
	}
	return detail, nil
}
