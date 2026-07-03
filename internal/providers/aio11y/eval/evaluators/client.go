package evaluators

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
)

const (
	basePath         = "/eval/evaluators"
	evaluatorByIDFmt = basePath + "/%s"
	evalTestPath     = "/eval:test"
)

// Client is an HTTP client for AI Observability evaluator endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new evaluator client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns all evaluators (paginated).
func (c *Client) List(ctx context.Context) ([]eval.EvaluatorDefinition, error) {
	return aio11yhttp.ListAll[eval.EvaluatorDefinition](ctx, c.base, basePath, nil)
}

// Get returns a single evaluator by ID.
func (c *Client) Get(ctx context.Context, id string) (*eval.EvaluatorDefinition, error) {
	evaluator, err := aio11yhttp.DoJSON[any, eval.EvaluatorDefinition](ctx, c.base, http.MethodGet, fmt.Sprintf(evaluatorByIDFmt, url.PathEscape(id)), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &evaluator, nil
}

// Create creates or updates an evaluator (POST is create-or-update).
func (c *Client) Create(ctx context.Context, evaluator *eval.EvaluatorDefinition) (*eval.EvaluatorDefinition, error) {
	created, err := aio11yhttp.DoJSON[eval.EvaluatorDefinition, eval.EvaluatorDefinition](ctx, c.base, http.MethodPost, basePath, evaluator, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// RunTest executes a one-shot eval:test against a generation.
func (c *Client) RunTest(ctx context.Context, req *eval.EvalTestRequest) (*eval.EvalTestResponse, error) {
	result, err := aio11yhttp.DoJSON[eval.EvalTestRequest, eval.EvalTestResponse](ctx, c.base, http.MethodPost, evalTestPath, req, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// Delete soft-deletes an evaluator by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	return aio11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, fmt.Sprintf(evaluatorByIDFmt, url.PathEscape(id)), nil, http.StatusOK, http.StatusNoContent)
}
