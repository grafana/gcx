package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/sigil/eval"
	"github.com/grafana/gcx/internal/providers/sigil/sigilhttp"
)

// Client is an HTTP client for Sigil eval judge endpoints.
type Client struct {
	base *sigilhttp.Client
}

// NewClient creates a new judge client.
func NewClient(base *sigilhttp.Client) *Client {
	return &Client{base: base}
}

// ListProviders returns available judge providers.
func (c *Client) ListProviders(ctx context.Context) ([]eval.JudgeProvider, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, "/eval/judge/providers", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list judge providers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sigilhttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Providers []eval.JudgeProvider `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode judge providers response: %w", err)
	}
	return envelope.Providers, nil
}

// ListModels returns available models, optionally filtered by provider.
func (c *Client) ListModels(ctx context.Context, provider string) ([]eval.JudgeModel, error) {
	query := url.Values{}
	if provider != "" {
		query.Set("provider", provider)
	}

	path := "/eval/judge/models"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := c.base.DoRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list judge models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, sigilhttp.HandleErrorResponse(resp)
	}

	var envelope struct {
		Models []eval.JudgeModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("failed to decode judge models response: %w", err)
	}
	return envelope.Models, nil
}
