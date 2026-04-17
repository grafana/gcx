package annotations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/config"
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

	var result []Annotation
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Get retrieves a single annotation by ID.
func (c *Client) Get(ctx context.Context, id int64) (*Annotation, error) {
	path := fmt.Sprintf("%s/%d", basePath, id)
	var result Annotation
	if err := c.doRequest(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Create creates a new annotation. On success, a.ID is populated with the
// server-assigned identifier.
func (c *Client) Create(ctx context.Context, a *Annotation) error {
	var result struct {
		ID      int64  `json:"id"`
		Message string `json:"message"`
	}
	if err := c.doRequest(ctx, http.MethodPost, basePath, a, &result); err != nil {
		return err
	}
	a.ID = result.ID
	return nil
}

// Update patches an existing annotation. The patch map may include any subset
// of text, tags, time, timeEnd.
func (c *Client) Update(ctx context.Context, id int64, patch map[string]any) error {
	path := fmt.Sprintf("%s/%d", basePath, id)
	var result struct {
		Message string `json:"message"`
	}
	return c.doRequest(ctx, http.MethodPatch, path, patch, &result)
}

// Delete removes an annotation by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	path := fmt.Sprintf("%s/%d", basePath, id)
	var result struct {
		Message string `json:"message"`
	}
	return c.doRequest(ctx, http.MethodDelete, path, nil, &result)
}

// doRequest performs an HTTP request and decodes the response into out.
// If body is non-nil it is JSON-encoded as the request body. If out is nil
// the response body is discarded.
func (c *Client) doRequest(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to encode request body: %w", err)
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return handleErrorResponse(resp)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

// handleErrorResponse reads an error response body and returns a formatted error.
func handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Message != "" {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, errResp.Message)
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}
	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
