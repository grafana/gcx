// Package humanevaluators lists the human evaluators a review queue's rubric
// can be built from.
package humanevaluators

import (
	"context"

	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
	"github.com/grafana/gcx/internal/providers/agento11y/eval"
)

const basePath = "/eval/human-evaluators"

// RubricItem is one human evaluator as a rubric question: the latest version
// of the evaluator, its instructions and its single output key.
type RubricItem struct {
	EvaluatorID      string         `json:"evaluator_id"`
	EvaluatorVersion string         `json:"evaluator_version"`
	Description      string         `json:"description,omitempty"`
	Instructions     string         `json:"instructions,omitempty"`
	OutputKey        eval.OutputKey `json:"output_key"`
}

// Client is an HTTP client for the human-evaluators endpoint.
type Client struct {
	base *agento11yhttp.Client
}

// NewClient creates a new human evaluator client.
func NewClient(base *agento11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns the latest version of every human evaluator, sorted by ID.
func (c *Client) List(ctx context.Context) ([]RubricItem, error) {
	return agento11yhttp.ListAll[RubricItem](ctx, c.base, basePath, nil)
}
