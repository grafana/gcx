package scores

import (
	"context"
	"net/url"
	"strconv"

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

// ListByGeneration returns scores for a generation, capped at limit (0 = no cap).
func (c *Client) ListByGeneration(ctx context.Context, generationID string, limit int) ([]Score, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	return aio11yhttp.ListAll[Score](ctx, c.base, "/query/generations/"+url.PathEscape(generationID)+"/scores", query, limit)
}
