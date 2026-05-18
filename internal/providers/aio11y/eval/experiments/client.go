package experiments

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/providers/aio11y/aio11yhttp"
)

const basePath = "/eval/experiments"

// ErrNotFound is returned by per-run methods (Get, Update, Cancel, GetReport)
// when the server responds with 404 so callers can distinguish a missing run
// from other API errors.
var ErrNotFound = errors.New("experiment not found")

// Client wraps the AI Observability plugin proxy with experiment-specific endpoints.
type Client struct {
	base *aio11yhttp.Client
}

// NewClient creates a new experiments client.
func NewClient(base *aio11yhttp.Client) *Client {
	return &Client{base: base}
}

// List returns experiments, paginated. Pass 0 for no limit.
func (c *Client) List(ctx context.Context, limit int) ([]Experiment, error) {
	return aio11yhttp.ListAll[Experiment](ctx, c.base, basePath, nil, limit)
}

// Get returns a single experiment by run ID.
func (c *Client) Get(ctx context.Context, runID string) (*Experiment, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(runID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get experiment %s: %w", runID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", runID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var exp Experiment
	if err := json.NewDecoder(resp.Body).Decode(&exp); err != nil {
		return nil, fmt.Errorf("failed to decode experiment response: %w", err)
	}
	return &exp, nil
}

// Create creates a new experiment.
func (c *Client) Create(ctx context.Context, exp *Experiment) (*Experiment, error) {
	body, err := json.Marshal(exp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal create request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create experiment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var created Experiment
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("failed to decode experiment response: %w", err)
	}
	return &created, nil
}

// Update sends a partial PATCH against an existing experiment.
func (c *Client) Update(ctx context.Context, runID string, req *UpdateRequest) (*Experiment, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal update request: %w", err)
	}

	resp, err := c.base.DoRequest(ctx, http.MethodPatch, basePath+"/"+url.PathEscape(runID), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to update experiment %s: %w", runID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", runID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var exp Experiment
	if err := json.NewDecoder(resp.Body).Decode(&exp); err != nil {
		return nil, fmt.Errorf("failed to decode experiment response: %w", err)
	}
	return &exp, nil
}

// Cancel transitions a running experiment to a canceled state.
//
// The plugin proxy matches the `:cancel` suffix on the run ID segment
// (single-segment path), not `/cancel`.
func (c *Client) Cancel(ctx context.Context, runID string) error {
	resp, err := c.base.DoRequest(ctx, http.MethodPost, basePath+"/"+url.PathEscape(runID)+":cancel", nil)
	if err != nil {
		return fmt.Errorf("failed to cancel experiment %s: %w", runID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%s: %w", runID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusAccepted {
		return aio11yhttp.HandleErrorResponse(resp)
	}
	return nil
}

// ListScores returns scores associated with a single experiment run.
func (c *Client) ListScores(ctx context.Context, runID string, limit int) ([]ScoreItem, error) {
	path := basePath + "/" + url.PathEscape(runID) + "/scores"
	return aio11yhttp.ListAll[ScoreItem](ctx, c.base, path, nil, limit)
}

// GetReport returns the aggregate report for an experiment run.
func (c *Client) GetReport(ctx context.Context, runID string) (*ExperimentReport, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, basePath+"/"+url.PathEscape(runID)+"/report", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get experiment report %s: %w", runID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", runID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, aio11yhttp.HandleErrorResponse(resp)
	}

	var report ExperimentReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		return nil, fmt.Errorf("failed to decode experiment report: %w", err)
	}
	return &report, nil
}
