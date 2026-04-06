package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
)

const basePath = "/eval/rules"

// Client is an HTTP client for Sigil rule endpoints.
type Client struct {
	base *sigilhttp.Client
}

// NewClient creates a new rule client.
func NewClient(base *sigilhttp.Client) *Client {
	return &Client{base: base}
}

// List returns all rules (paginated).
func (c *Client) List(ctx context.Context) ([]eval.RuleDefinition, error) {
	return sigilhttp.ListAll[eval.RuleDefinition](ctx, c.base, basePath, nil)
}

// Get returns a single rule by ID.
func (c *Client) Get(ctx context.Context, id string) (*eval.RuleDefinition, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get rule %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sigilhttp.HandleErrorResponse(resp)
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
		return nil, sigilhttp.HandleErrorResponse(resp)
	}

	var created eval.RuleDefinition
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode rule response: %w", err)
	}
	return &created, nil
}

// Update patches an existing rule with raw JSON.
// Only the fields present in the input are included in the PATCH request.
func (c *Client) Update(ctx context.Context, id string, body []byte) (*eval.RuleDefinition, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodPatch, basePath+"/"+url.PathEscape(id), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sigilhttp.HandleErrorResponse(resp)
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
		return sigilhttp.HandleErrorResponse(resp)
	}
	return nil
}
