package rules

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
)

const (
	basePath    = "/eval/rules"
	ruleByIDFmt = basePath + "/%s"
)

// Client is an HTTP client for Agent Observability rule endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new rule client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns all rules (paginated).
func (c *Client) List(ctx context.Context) ([]eval.RuleDefinition, error) {
	return aio11yhttp.ListAll[eval.RuleDefinition](ctx, c.base, basePath, nil)
}

// Get returns a single rule by ID.
func (c *Client) Get(ctx context.Context, id string) (*eval.RuleDefinition, error) {
	rule, err := aio11yhttp.DoJSON[any, eval.RuleDefinition](ctx, c.base, http.MethodGet, fmt.Sprintf(ruleByIDFmt, url.PathEscape(id)), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// Create creates a new rule.
func (c *Client) Create(ctx context.Context, rule *eval.RuleDefinition) (*eval.RuleDefinition, error) {
	created, err := aio11yhttp.DoJSON[eval.RuleDefinition, eval.RuleDefinition](ctx, c.base, http.MethodPost, basePath, rule, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update sends a full rule definition as a PATCH request.
func (c *Client) Update(ctx context.Context, id string, rule *eval.RuleDefinition) (*eval.RuleDefinition, error) {
	updated, err := aio11yhttp.DoJSON[eval.RuleDefinition, eval.RuleDefinition](ctx, c.base, http.MethodPatch, fmt.Sprintf(ruleByIDFmt, url.PathEscape(id)), rule, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete deletes a rule by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	return aio11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, fmt.Sprintf(ruleByIDFmt, url.PathEscape(id)), nil, http.StatusOK, http.StatusNoContent)
}
