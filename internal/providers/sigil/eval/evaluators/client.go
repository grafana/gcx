package evaluators

import (
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
