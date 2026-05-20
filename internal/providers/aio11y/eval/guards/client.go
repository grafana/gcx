package guards

import (
	"bytes"
	"context"
	"encoding/json"
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
	resp, err := c.base.DoRequest(ctx, http.MethodGet, fmt.Sprintf(hookRuleByIDFmt, url.PathEscape(id)), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get hook rule %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("hook rule %s: %w", id, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var rule eval.HookRuleDefinition
	if err := json.NewDecoder(resp.Body).Decode(&rule); err != nil {
		return nil, fmt.Errorf("failed to decode hook rule response: %w", err)
	}
	return &rule, nil
}

// Create creates a new hook rule.
func (c *Client) Create(ctx context.Context, rule *eval.HookRuleDefinition) (*eval.HookRuleDefinition, error) {
	body, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal hook rule: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create hook rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var created eval.HookRuleDefinition
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode hook rule response: %w", err)
	}
	return &created, nil
}

// Update replaces a hook rule with the full definition. The hook-rules API
// does not support PATCH; omitted fields reset to server defaults, so callers
// must send the complete state.
func (c *Client) Update(ctx context.Context, id string, rule *eval.HookRuleDefinition) (*eval.HookRuleDefinition, error) {
	body, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal hook rule: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPut, fmt.Sprintf(hookRuleByIDFmt, url.PathEscape(id)), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update hook rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var updated eval.HookRuleDefinition
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("failed to decode hook rule response: %w", err)
	}
	return &updated, nil
}

// Delete deletes a hook rule by ID.
//
// Sigil returns 204 No Content on success; 200 is also accepted for forward
// compatibility.
func (c *Client) Delete(ctx context.Context, id string) error {
	resp, err := c.base.DoRequest(ctx, http.MethodDelete, fmt.Sprintf(hookRuleByIDFmt, url.PathEscape(id)), nil)
	if err != nil {
		return fmt.Errorf("failed to delete hook rule %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}
