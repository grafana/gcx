package generations

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

const (
	generationsPath   = "/query/generations"
	generationByIDFmt = generationsPath + "/%s"
)

// Client is an HTTP client for Agent Observability generation endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new generations client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// Get returns a single generation by ID.
func (c *Client) Get(ctx context.Context, id string) (map[string]any, error) {
	return aio11yhttp.DoJSON[any, map[string]any](ctx, c.base, http.MethodGet, fmt.Sprintf(generationByIDFmt, url.PathEscape(id)), nil, http.StatusOK)
}
