package aio11yhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"k8s.io/client-go/rest"
)

const pluginBasePath = "/api/plugins/grafana-sigil-app/resources"

// Client is a base HTTP client for the AI Observability plugin API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new AI Observability client from a Grafana REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// listResponse is the common envelope for all paginated list endpoints.
type listResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// DoRequest builds and executes an HTTP request against the AI Observability plugin API.
func (c *Client) DoRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.restConfig.Host+pluginBasePath+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

// ListAll fetches pages from a paginated endpoint up to maxItems,
// using the common { "items": [...], "next_cursor": "..." } envelope.
// Pass maxItems <= 0 for no limit (fetch all pages).
func ListAll[T any](ctx context.Context, c *Client, basePath string, query url.Values, maxItems ...int) ([]T, error) {
	limit := 0
	if len(maxItems) > 0 {
		limit = maxItems[0]
	}
	var all []T

	// Copy to avoid mutating the caller's map during pagination.
	q := make(url.Values, len(query))
	maps.Copy(q, query)

	for {
		path := basePath
		if encoded := q.Encode(); encoded != "" {
			path += "?" + encoded
		}

		resp, err := c.DoRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			err := HandleErrorResponse(resp)
			resp.Body.Close()
			return nil, err
		}

		var page listResponse[T]
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		all = append(all, page.Items...)

		if limit > 0 && len(all) >= limit {
			all = all[:limit]
			break
		}

		if page.NextCursor == "" {
			break
		}
		q.Set("cursor", page.NextCursor)
	}

	if all == nil {
		return []T{}, nil
	}
	return all, nil
}

// DoJSON executes an HTTP request against the plugin API, optionally
// marshaling body as the JSON request payload, and decodes a JSON response
// into Resp. Pass a nil body for requests with no request body (e.g.
// GET/DELETE) — in that case Req can be instantiated as `any`.
//
// Any response status not present in okStatuses is treated as an error via
// HandleErrorResponse.
func DoJSON[Req, Resp any](ctx context.Context, c *Client, method, path string, body *Req, okStatuses ...int) (Resp, error) {
	return doJSON[Req, Resp](ctx, c, method, path, body, nil, okStatuses)
}

// DoJSONNotFound behaves like DoJSON, but returns notFoundErr instead of the
// generic HandleErrorResponse error when the response status is 404.
// Callers typically pass a sentinel wrapped with request-specific context,
// e.g. fmt.Errorf("%s: %w", id, ErrNotFound), so that errors.Is still
// matches the resource-specific sentinel.
func DoJSONNotFound[Req, Resp any](ctx context.Context, c *Client, method, path string, body *Req, notFoundErr error, okStatuses ...int) (Resp, error) {
	return doJSON[Req, Resp](ctx, c, method, path, body, notFoundErr, okStatuses)
}

func doJSON[Req, Resp any](ctx context.Context, c *Client, method, path string, body *Req, notFoundErr error, okStatuses []int) (Resp, error) {
	var zero Resp

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return zero, fmt.Errorf("failed to marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	resp, err := c.DoRequest(ctx, method, path, reader)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()

	if notFoundErr != nil && resp.StatusCode == http.StatusNotFound {
		return zero, notFoundErr
	}
	if !slices.Contains(okStatuses, resp.StatusCode) {
		return zero, HandleErrorResponse(resp)
	}

	var result Resp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return zero, fmt.Errorf("failed to decode response: %w", err)
	}
	return result, nil
}

// DoStatus executes an HTTP request against the plugin API, optionally
// marshaling body as the JSON request payload, and checks that the response
// status is one of okStatuses without decoding a response body. Use this for
// endpoints that return no useful body (e.g. DELETE, or actions like
// cancel/add-members).
func DoStatus[Req any](ctx context.Context, c *Client, method, path string, body *Req, okStatuses ...int) error {
	return doStatus[Req](ctx, c, method, path, body, nil, okStatuses)
}

// DoStatusNotFound behaves like DoStatus, but returns notFoundErr instead of
// the generic HandleErrorResponse error when the response status is 404.
func DoStatusNotFound[Req any](ctx context.Context, c *Client, method, path string, body *Req, notFoundErr error, okStatuses ...int) error {
	return doStatus[Req](ctx, c, method, path, body, notFoundErr, okStatuses)
}

func doStatus[Req any](ctx context.Context, c *Client, method, path string, body *Req, notFoundErr error, okStatuses []int) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	resp, err := c.DoRequest(ctx, method, path, reader)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if notFoundErr != nil && resp.StatusCode == http.StatusNotFound {
		return notFoundErr
	}
	if !slices.Contains(okStatuses, resp.StatusCode) {
		return HandleErrorResponse(resp)
	}
	return nil
}

// NewClientFromCommand creates a Client from a cobra command and config loader.
func NewClientFromCommand(cmd *cobra.Command, loader *providers.ConfigLoader) (*Client, error) {
	cfg, err := loader.LoadGrafanaConfig(cmd.Context())
	if err != nil {
		return nil, err
	}
	return NewClient(cfg)
}

// FormatTime formats a time for table display, returning "-" for zero values.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

// Truncate returns s shortened to maxLen runes, adding "..." if truncated.
// Returns "-" for empty strings.
func Truncate(s string, maxLen int) string {
	if s == "" {
		return "-"
	}
	r := []rune(s)
	if len(r) > maxLen {
		return string(r[:maxLen-3]) + "..."
	}
	return s
}

// HandleErrorResponse reads an error response body and returns a formatted error.
func HandleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
