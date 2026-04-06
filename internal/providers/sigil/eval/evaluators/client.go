package evaluators

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

const basePath = "/eval/evaluators"

// Client is an HTTP client for Sigil evaluator endpoints.
type Client struct {
	base *sigilhttp.Client
}

// NewClient creates a new evaluator client.
func NewClient(base *sigilhttp.Client) *Client {
	return &Client{base: base}
}

// List returns all evaluators (paginated).
func (c *Client) List(ctx context.Context) ([]eval.EvaluatorDefinition, error) {
	return sigilhttp.ListAll[eval.EvaluatorDefinition](ctx, c.base, basePath, nil)
}

// Get returns a single evaluator by ID.
func (c *Client) Get(ctx context.Context, id string) (*eval.EvaluatorDefinition, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get evaluator %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sigilhttp.HandleErrorResponse(resp)
	}

	var evaluator eval.EvaluatorDefinition
	if err := json.NewDecoder(resp.Body).Decode(&evaluator); err != nil {
		return nil, fmt.Errorf("failed to decode evaluator response: %w", err)
	}
	return &evaluator, nil
}

// Create creates or updates an evaluator (POST is create-or-update).
func (c *Client) Create(ctx context.Context, evaluator *eval.EvaluatorDefinition) (*eval.EvaluatorDefinition, error) {
	body, err := json.Marshal(evaluator)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal evaluator: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create evaluator: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, sigilhttp.HandleErrorResponse(resp)
	}

	var created eval.EvaluatorDefinition
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode evaluator response: %w", err)
	}
	return &created, nil
}

// RunTest executes a one-shot eval:test against a generation.
func (c *Client) RunTest(ctx context.Context, req *eval.EvalTestRequest) (*eval.EvalTestResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal test request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, "/eval:test", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to run eval test: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sigilhttp.HandleErrorResponse(resp)
	}

	var result eval.EvalTestResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode test response: %w", err)
	}
	return &result, nil
}

// Delete soft-deletes an evaluator by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	resp, err := c.base.DoRequest(ctx, http.MethodDelete, basePath+"/"+url.PathEscape(id), nil)
	if err != nil {
		return fmt.Errorf("failed to delete evaluator %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return sigilhttp.HandleErrorResponse(resp)
	}
	return nil
}
