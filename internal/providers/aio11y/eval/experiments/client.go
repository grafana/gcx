package experiments

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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
	exp, err := aio11yhttp.DoJSONNotFound[any, Experiment](ctx, c.base, http.MethodGet, basePath+"/"+url.PathEscape(runID), nil,
		fmt.Errorf("%s: %w", runID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &exp, nil
}

// Create creates a new experiment.
func (c *Client) Create(ctx context.Context, exp *Experiment) (*Experiment, error) {
	created, err := aio11yhttp.DoJSON[Experiment, Experiment](ctx, c.base, http.MethodPost, basePath, exp, http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// Update sends a partial PATCH against an existing experiment.
func (c *Client) Update(ctx context.Context, runID string, req *UpdateRequest) (*Experiment, error) {
	exp, err := aio11yhttp.DoJSONNotFound[UpdateRequest, Experiment](ctx, c.base, http.MethodPatch, basePath+"/"+url.PathEscape(runID), req,
		fmt.Errorf("%s: %w", runID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &exp, nil
}

// Cancel transitions a running experiment to a canceled state.
//
// The plugin proxy matches the `:cancel` suffix on the run ID segment
// (single-segment path), not `/cancel`. url.PathEscape does not escape
// `:` (it's an allowed sub-delim in path segments), which would make the
// route ambiguous if a runID ever contained a literal colon, so we escape
// it manually before appending the action suffix.
func (c *Client) Cancel(ctx context.Context, runID string) error {
	escaped := strings.ReplaceAll(url.PathEscape(runID), ":", "%3A")
	return aio11yhttp.DoStatusNotFound[any](ctx, c.base, http.MethodPost, basePath+"/"+escaped+":cancel", nil,
		fmt.Errorf("%s: %w", runID, ErrNotFound), http.StatusOK, http.StatusNoContent, http.StatusAccepted)
}

// ListScores returns scores associated with a single experiment run.
func (c *Client) ListScores(ctx context.Context, runID string, limit int) ([]ScoreItem, error) {
	path := basePath + "/" + url.PathEscape(runID) + "/scores"
	return aio11yhttp.ListAll[ScoreItem](ctx, c.base, path, nil, limit)
}

// GetReport returns the aggregate report for an experiment run.
func (c *Client) GetReport(ctx context.Context, runID string) (*ExperimentReport, error) {
	report, err := aio11yhttp.DoJSONNotFound[any, ExperimentReport](ctx, c.base, http.MethodGet, basePath+"/"+url.PathEscape(runID)+"/report", nil,
		fmt.Errorf("%s: %w", runID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &report, nil
}
