package guards

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
	"github.com/grafana/gcx/internal/resources/adapter"
)

const (
	basePath        = "/eval/hook-rules"
	hookRuleByIDFmt = basePath + "/%s"
)

// ErrNotFound wraps adapter.ErrNotFound so resource push can create missing hook rules.
var ErrNotFound = fmt.Errorf("hook rule: %w", adapter.ErrNotFound)

// Client is an HTTP client for AI Observability hook-rule (guard) endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new guards client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns all hook rules (paginated).
func (c *Client) List(ctx context.Context) ([]eval.HookRuleDefinition, error) {
	return aio11yhttp.ListAll[eval.HookRuleDefinition](ctx, c.base, basePath, nil)
}

// Get returns a single hook rule by ID.
func (c *Client) Get(ctx context.Context, id string) (*eval.HookRuleDefinition, error) {
	rule, err := aio11yhttp.DoJSONNotFound[any, eval.HookRuleDefinition](ctx, c.base, http.MethodGet, fmt.Sprintf(hookRuleByIDFmt, url.PathEscape(id)), nil,
		fmt.Errorf("hook rule %s: %w", id, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// Create creates a new hook rule.
func (c *Client) Create(ctx context.Context, rule *eval.HookRuleDefinition) (*eval.HookRuleDefinition, error) {
	created, err := aio11yhttp.DoJSON[eval.HookRuleDefinition, eval.HookRuleDefinition](ctx, c.base, http.MethodPost, basePath, rule, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update replaces a hook rule with the full definition. The hook-rules API
// does not support PATCH; omitted fields reset to server defaults, so callers
// must send the complete state.
func (c *Client) Update(ctx context.Context, id string, rule *eval.HookRuleDefinition) (*eval.HookRuleDefinition, error) {
	updated, err := aio11yhttp.DoJSON[eval.HookRuleDefinition, eval.HookRuleDefinition](ctx, c.base, http.MethodPut, fmt.Sprintf(hookRuleByIDFmt, url.PathEscape(id)), rule, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete deletes a hook rule by ID.
//
// Sigil returns 204 No Content on success; 200 is also accepted for forward
// compatibility.
func (c *Client) Delete(ctx context.Context, id string) error {
	return aio11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, fmt.Sprintf(hookRuleByIDFmt, url.PathEscape(id)), nil, http.StatusOK, http.StatusNoContent)
}
