package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
)

const basePath = "/eval/templates"

// Client is an HTTP client for AI Observability eval template endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new templates client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns templates, optionally filtered by scope.
// An optional maxItems argument limits how many items are fetched (0 = no limit).
func (c *Client) List(ctx context.Context, scope string, maxItems ...int) ([]eval.TemplateDefinition, error) {
	query := url.Values{}
	if scope != "" {
		query.Set("scope", scope)
	}
	return aio11yhttp.ListAll[eval.TemplateDefinition](ctx, c.base, basePath, query, maxItems...)
}

// Get returns a single template by ID.
func (c *Client) Get(ctx context.Context, id string) (*eval.TemplateDetail, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get template %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var detail eval.TemplateDetail
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("failed to decode template response: %w", err)
	}
	return &detail, nil
}

// ListVersions returns version history for a template.
func (c *Client) ListVersions(ctx context.Context, id string) ([]eval.TemplateVersion, error) {
	return aio11yhttp.ListAll[eval.TemplateVersion](ctx, c.base, basePath+"/"+url.PathEscape(id)+"/versions", nil)
}
