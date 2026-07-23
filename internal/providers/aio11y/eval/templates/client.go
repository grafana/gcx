package templates

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
)

const (
	basePath        = "/eval/templates"
	templateByIDFmt = basePath + "/%s"
	versionsPathFmt = basePath + "/%s/versions"
)

// Client is an HTTP client for Agent Observability eval template endpoints.
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
	detail, err := aio11yhttp.DoJSON[any, eval.TemplateDetail](ctx, c.base, http.MethodGet, fmt.Sprintf(templateByIDFmt, url.PathEscape(id)), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}

// ListVersions returns version history for a template.
func (c *Client) ListVersions(ctx context.Context, id string) ([]eval.TemplateVersion, error) {
	return aio11yhttp.ListAll[eval.TemplateVersion](ctx, c.base, fmt.Sprintf(versionsPathFmt, url.PathEscape(id)), nil)
}
