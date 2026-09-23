package modelrates

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
)

const basePath = "/model-rates"

// Client is an HTTP client for the Agent Observability model rate endpoints.
type Client struct {
	base *agento11yhttp.Client
}

func NewClient(base *agento11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns every rate the tenant has configured, including superseded
// ones: a rate change records a new row rather than replacing the old, so the
// history is what a read path uses to price an older generation.
func (c *Client) List(ctx context.Context, maxItems ...int) ([]Rate, error) {
	return agento11yhttp.ListAll[Rate](ctx, c.base, basePath, nil, maxItems...)
}

// Set records a rate in force from now.
func (c *Client) Set(ctx context.Context, rate *RateWrite) (*Rate, error) {
	stored, err := agento11yhttp.DoJSON[RateWrite, Rate](ctx, c.base, http.MethodPost, basePath, rate, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &stored, nil
}

// Delete removes one rate. It takes the full key including effectiveFrom
// because a model can carry several rates, one per change, and a delete has to
// name which. The timestamp comes from List.
func (c *Client) Delete(ctx context.Context, provider, model string, effectiveFrom time.Time) error {
	query := url.Values{}
	query.Set("provider", provider)
	query.Set("model", model)
	query.Set("effective_from", effectiveFrom.UTC().Format(time.RFC3339Nano))
	return agento11yhttp.DoStatus[any](ctx, c.base, http.MethodDelete, basePath+"?"+query.Encode(), nil, http.StatusOK, http.StatusNoContent)
}
