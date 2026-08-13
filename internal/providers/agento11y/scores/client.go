package scores

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
)

const generationScoresPathFmt = "/query/generations/%s/scores"

// Client is an HTTP client for Agent Observability generation score endpoints.
type Client struct {
	base *agento11yhttp.Client
}

// NewClient creates a new scores client.
func NewClient(base *agento11yhttp.Client) *Client {
	return &Client{base: base}
}

// ListByGeneration returns scores for a generation, paginated.
func (c *Client) ListByGeneration(ctx context.Context, generationID string, limit int) ([]Score, error) {
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	return agento11yhttp.ListAll[Score](ctx, c.base, fmt.Sprintf(generationScoresPathFmt, url.PathEscape(generationID)), query)
}
