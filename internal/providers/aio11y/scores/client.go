package scores

import (
	"context"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

// Client is an HTTP client for AI Observability generation score endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new scores client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// ListByGeneration returns scores for a generation.
func (c *Client) ListByGeneration(ctx context.Context, generationID string) ([]Score, error) {
	return aio11yhttp.ListAll[Score](ctx, c.base, "/query/generations/"+url.PathEscape(generationID)+"/scores", nil)
}
