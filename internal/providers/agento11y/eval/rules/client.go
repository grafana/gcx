package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
)

const (
	basePath    = "/eval/rules"
	ruleByIDFmt = basePath + "/%s"
)

func actionPath(ruleID string) string {
	return fmt.Sprintf(ruleByIDFmt, url.PathEscape(ruleID)) + "/actions"
}

func actionByIDPath(ruleID, actionID string) string {
	return actionPath(ruleID) + "/" + url.PathEscape(actionID)
}

// Client is an HTTP client for Agent Observability rule endpoints.
type Client struct {
	base *agento11yhttp.Client
}

// NewClient creates a new rule client.
func NewClient(base *agento11yhttp.Client) *Client {
	return &Client{base: base}
}

// NewClientForLoader creates a rules client using the command's selected
// Grafana configuration.
func NewClientForLoader(ctx context.Context, loader *providers.ConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load REST config for Agent Observability rules: %w", err)
	}
	base, err := agento11yhttp.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create Agent Observability HTTP client: %w", err)
	}
	return NewClient(base), nil
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
	body, err := ruleBody(rule, true)
	if err != nil {
		return nil, err
	}
	created, err := agento11yhttp.DoJSON[map[string]any, eval.RuleDefinition](ctx, c.base, http.MethodPost, basePath, &body, http.StatusOK, http.StatusCreated)
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
	body, err := ruleBody(rule, false)
	if err != nil {
		return nil, err
	}

	updated, err := agento11yhttp.DoJSON[map[string]any, eval.RuleDefinition](ctx, c.base, http.MethodPatch, fmt.Sprintf(ruleByIDFmt, url.PathEscape(id)), &body, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func ruleBody(rule *eval.RuleDefinition, includeID bool) (map[string]any, error) {
	raw, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal rule: %w", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("failed to prepare rule body: %w", err)
	}
	delete(body, "actions")
	if !includeID {
		delete(body, "rule_id")
	}
	return body, nil
}

func (c *Client) ListActions(ctx context.Context, ruleID string) ([]eval.RuleAction, error) {
	return agento11yhttp.ListAll[eval.RuleAction](ctx, c.base, actionPath(ruleID), nil)
}

func (c *Client) GetAction(ctx context.Context, ruleID, actionID string) (*eval.RuleAction, error) {
	action, err := agento11yhttp.DoJSON[any, eval.RuleAction](ctx, c.base, http.MethodGet, actionByIDPath(ruleID, actionID), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &action, nil
}

type actionCreateRequest struct {
	Condition    eval.RuleActionCondition `json:"condition"`
	ActionConfig eval.RuleActionConfig    `json:"action_config"`
	Enabled      *bool                    `json:"enabled,omitempty"`
}

type actionUpdateRequest struct {
	Condition    *eval.RuleActionCondition `json:"condition,omitempty"`
	ActionConfig *eval.RuleActionConfig    `json:"action_config,omitempty"`
	Enabled      *bool                     `json:"enabled,omitempty"`
}

func (c *Client) CreateAction(ctx context.Context, ruleID string, action *eval.RuleAction) (*eval.RuleAction, error) {
	req := actionCreateRequest{Condition: action.Condition, ActionConfig: action.ActionConfig, Enabled: &action.Enabled}
	created, err := agento11yhttp.DoJSON[actionCreateRequest, eval.RuleAction](ctx, c.base, http.MethodPost, actionPath(ruleID), &req, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func (c *Client) UpdateAction(ctx context.Context, ruleID string, action *eval.RuleAction) (*eval.RuleAction, error) {
	req := actionUpdateRequest{Condition: &action.Condition, ActionConfig: &action.ActionConfig, Enabled: &action.Enabled}
	updated, err := agento11yhttp.DoJSON[actionUpdateRequest, eval.RuleAction](ctx, c.base, http.MethodPatch, actionByIDPath(ruleID, action.ActionID), &req, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (c *Client) DeleteAction(ctx context.Context, ruleID, actionID string) error {
	return agento11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, actionByIDPath(ruleID, actionID), nil, http.StatusOK, http.StatusNoContent)
}

// Delete deletes a rule by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	return agento11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, fmt.Sprintf(ruleByIDFmt, url.PathEscape(id)), nil, http.StatusOK, http.StatusNoContent)
}
