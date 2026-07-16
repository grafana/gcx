package judge

import (
	"context"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
	"github.com/grafana/gcx/internal/providers/aio11y/eval"
)

const (
	judgeProvidersPath = "/eval/judge/providers"
	judgeModelsPath    = "/eval/judge/models"
)

// Client is an HTTP client for Agent Observability eval judge endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new judge client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// providersEnvelope is the response envelope for the judge providers endpoint.
type providersEnvelope struct {
	Providers []eval.JudgeProvider `json:"providers"`
}

// modelsEnvelope is the response envelope for the judge models endpoint.
type modelsEnvelope struct {
	Models []eval.JudgeModel `json:"models"`
}

// ListProviders returns available judge providers.
func (c *Client) ListProviders(ctx context.Context) ([]eval.JudgeProvider, error) {
	envelope, err := aio11yhttp.DoJSON[any, providersEnvelope](ctx, c.base, http.MethodGet, judgeProvidersPath, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return envelope.Providers, nil
}

// ListModels returns available models, optionally filtered by provider.
func (c *Client) ListModels(ctx context.Context, provider string) ([]eval.JudgeModel, error) {
	query := url.Values{}
	if provider != "" {
		query.Set("provider", provider)
	}

	path := judgeModelsPath
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	envelope, err := aio11yhttp.DoJSON[any, modelsEnvelope](ctx, c.base, http.MethodGet, path, nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return envelope.Models, nil
}
