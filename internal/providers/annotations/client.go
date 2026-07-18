package annotations

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/coreapi"
)

const basePath = "/api/annotations"

// Client is a Grafana annotations API client.
type Client struct {
	base *coreapi.Client
}

// NewClient creates a new annotations client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	base, err := coreapi.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{base: base}, nil
}

// ListOptions configures filtering for List operations.
// From and To are epoch milliseconds.
type ListOptions struct {
	From  int64
	To    int64
	Tags  []string
	Limit int
}

// List returns annotations matching the given options.
func (c *Client) List(ctx context.Context, opts ListOptions) ([]Annotation, error) {
	params := url.Values{}
	if opts.From > 0 {
		params.Set("from", strconv.FormatInt(opts.From, 10))
	}
	if opts.To > 0 {
		params.Set("to", strconv.FormatInt(opts.To, 10))
	}
	for _, tag := range opts.Tags {
		params.Add("tags", tag)
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}

	path := basePath
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	return coreapi.DoJSON[[]Annotation](ctx, c.base, http.MethodGet, path, nil, http.StatusOK)
}

// Get retrieves a single annotation by ID.
func (c *Client) Get(ctx context.Context, id int64) (*Annotation, error) {
	a, err := coreapi.DoJSON[Annotation](ctx, c.base, http.MethodGet, fmt.Sprintf("%s/%d", basePath, id), nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Create creates a new annotation. On success, a.ID is populated with the
// server-assigned identifier.
func (c *Client) Create(ctx context.Context, a *Annotation) error {
	type createResp struct {
		ID int64 `json:"id"`
	}
	res, err := coreapi.DoJSON[createResp](ctx, c.base, http.MethodPost, basePath, a, http.StatusOK)
	if err != nil {
		return err
	}
	a.ID = res.ID
	return nil
}

// Update patches an existing annotation. The patch map may include any subset
// of text, tags, time, timeEnd.
func (c *Client) Update(ctx context.Context, id int64, patch map[string]any) error {
	return coreapi.DoStatus(ctx, c.base, http.MethodPatch, fmt.Sprintf("%s/%d", basePath, id), &patch, http.StatusOK)
}

// Delete removes an annotation by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	return coreapi.DoStatus(ctx, c.base, http.MethodDelete, fmt.Sprintf("%s/%d", basePath, id), nil, http.StatusOK)
}

// Tags returns the annotation tags known to the org, with usage counts.
func (c *Client) Tags(ctx context.Context) ([]AnnotationTag, error) {
	res, err := coreapi.DoJSON[tagsResponse](ctx, c.base, http.MethodGet, basePath+"/tags", nil, http.StatusOK)
	if err != nil {
		return nil, err
	}
	return res.Result.Tags, nil
}

// MassDelete deletes multiple annotations selected by the given request
// (by annotation ID, or by dashboard + panel).
func (c *Client) MassDelete(ctx context.Context, req MassDeleteRequest) error {
	return coreapi.DoStatus(ctx, c.base, http.MethodPost, basePath+"/mass-delete", &req, http.StatusOK)
}
