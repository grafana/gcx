package annotations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"k8s.io/client-go/rest"
)

const basePath = "/api/annotations"

// Client is a Grafana annotations API client.
type Client struct {
	httpClient *http.Client
	host       string
}

// NewClient creates a new annotations client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

// doJSON sends a request through the configured Grafana transport. It accepts
// nil response targets for endpoints that only return a status message.
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to encode request: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return providers.HandleErrorResponse(resp)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}
	return nil
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

	var annotations []Annotation
	err := c.doJSON(ctx, http.MethodGet, path, nil, &annotations)
	return annotations, err
}

// Get retrieves a single annotation by ID.
func (c *Client) Get(ctx context.Context, id int64) (*Annotation, error) {
	var a Annotation
	err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("%s/%d", basePath, id), nil, &a)
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
	var res createResp
	err := c.doJSON(ctx, http.MethodPost, basePath, a, &res)
	if err != nil {
		return err
	}
	a.ID = res.ID
	return nil
}

// Update patches an existing annotation. The patch map may include any subset
// of text, tags, time, timeEnd.
func (c *Client) Update(ctx context.Context, id int64, patch map[string]any) error {
	return c.doJSON(ctx, http.MethodPatch, fmt.Sprintf("%s/%d", basePath, id), patch, nil)
}

// Delete removes an annotation by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	return c.doJSON(ctx, http.MethodDelete, fmt.Sprintf("%s/%d", basePath, id), nil, nil)
}

// Tags returns up to limit annotation tags known to the org, with usage counts.
func (c *Client) Tags(ctx context.Context, limit int) ([]AnnotationTag, error) {
	var res tagsResponse
	err := c.doJSON(ctx, http.MethodGet, basePath+"/tags?limit="+strconv.Itoa(limit), nil, &res)
	if err != nil {
		return nil, err
	}
	return res.Result.Tags, nil
}

// MassDelete deletes multiple annotations selected by the given request
// (by annotation ID, or by dashboard + panel).
func (c *Client) MassDelete(ctx context.Context, req MassDeleteRequest) error {
	return c.doJSON(ctx, http.MethodPost, basePath+"/mass-delete", req, nil)
}
