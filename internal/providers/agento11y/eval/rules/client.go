package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
)

const (
	basePath    = "/eval/rules"
	ruleByIDFmt = basePath + "/%s"
)

// Client is an HTTP client for Agent Observability rule endpoints.
type Client struct {
	base *agento11yhttp.Client
}

// NewClient creates a new rule client.
func NewClient(base *agento11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns all rules (paginated).
func (c *Client) List(ctx context.Context) ([]eval.RuleDefinition, error) {
	return agento11yhttp.ListAll[eval.RuleDefinition](ctx, c.base, basePath, nil)
}

// Get returns a single rule by ID.
func (c *Client) Get(ctx context.Context, id string) (*eval.RuleDefinition, error) {
	rule, err := agento11yhttp.DoJSON[any, eval.RuleDefinition](ctx, c.base, http.MethodGet, fmt.Sprintf(ruleByIDFmt, url.PathEscape(id)), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &rule, nil
}

// Create creates a new rule.
func (c *Client) Create(ctx context.Context, rule *eval.RuleDefinition) (*eval.RuleDefinition, error) {
	created, err := agento11yhttp.DoJSON[eval.RuleDefinition, eval.RuleDefinition](ctx, c.base, http.MethodPost, basePath, rule, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update sends a rule definition as a PATCH request.
//
// The rule id is taken from the URL path, so it must not also appear in the
// body: the server decodes the PATCH body with DisallowUnknownFields and its
// update DTO has no rule_id field, so a body carrying rule_id (even as an empty
// string) fails with `unknown field "rule_id"`. RuleDefinition.RuleID has no
// omitempty because it is required on create, so we can't just zero it — we
// marshal to a map and drop the key entirely before sending.
func (c *Client) Update(ctx context.Context, id string, rule *eval.RuleDefinition) (*eval.RuleDefinition, error) {
	raw, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rule: %w", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("failed to prepare rule body: %w", err)
	}
	delete(body, "rule_id")

	updated, err := agento11yhttp.DoJSON[map[string]any, eval.RuleDefinition](ctx, c.base, http.MethodPatch, fmt.Sprintf(ruleByIDFmt, url.PathEscape(id)), &body, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete deletes a rule by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	return agento11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, fmt.Sprintf(ruleByIDFmt, url.PathEscape(id)), nil, http.StatusOK, http.StatusNoContent)
}
