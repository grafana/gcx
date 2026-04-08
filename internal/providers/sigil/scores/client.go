package scores

import (
	"context"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
)

// Client is an HTTP client for Sigil generation score endpoints.
type Client struct {
	base *sigilhttp.Client
}

// NewClient creates a new scores client.
func NewClient(base *sigilhttp.Client) *Client {
	return &Client{base: base}
}

// ListByGeneration returns scores for a generation, paginated.
func (c *Client) ListByGeneration(ctx context.Context, generationID string, limit int) ([]Score, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	return sigilhttp.ListAll[Score](ctx, c.base, "/query/generations/"+url.PathEscape(generationID)+"/scores", query)
}
