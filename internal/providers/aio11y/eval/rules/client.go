package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
)

const basePath = "/eval/rules"

// Client is an HTTP client for AI Observability rule endpoints.
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
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get rule %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var rule eval.RuleDefinition
	if err := json.NewDecoder(resp.Body).Decode(&rule); err != nil {
		return nil, fmt.Errorf("failed to decode rule response: %w", err)
	}
	return &rule, nil
}

// Create creates a new rule.
func (c *Client) Create(ctx context.Context, rule *eval.RuleDefinition) (*eval.RuleDefinition, error) {
	body, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rule: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var created eval.RuleDefinition
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode rule response: %w", err)
	}
	return &created, nil
}

// Update sends a full rule definition as a PATCH request.
func (c *Client) Update(ctx context.Context, id string, rule *eval.RuleDefinition) (*eval.RuleDefinition, error) {
	body, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rule: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPatch, basePath+"/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var updated eval.RuleDefinition
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("failed to decode rule response: %w", err)
	}
	return &updated, nil
}

// Delete deletes a rule by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	resp, err := c.base.DoRequest(ctx, http.MethodDelete, basePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("failed to delete rule %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}
