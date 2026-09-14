// Package coreapi provides a shared HTTP client and generic request helpers for
// gcx providers that talk to Grafana's core HTTP API (the legacy /api/* REST
// endpoints, e.g. /api/annotations, /api/org/users, /api/access-control/...).
//
// It exists so individual providers do not each re-implement the same
// marshal / execute / status-check / decode boilerplate. Callers pass the full
// request path (including the /api prefix); the client prepends the configured
// Grafana host.
package coreapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"k8s.io/client-go/rest"
)

// Client is a base HTTP client for Grafana's core /api/* endpoints.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new core API client from a Grafana REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{restConfig: cfg, httpClient: httpClient}, nil
}

// DoRequest builds and executes an HTTP request against the core API. The path
// is appended to the configured host (e.g. "/api/annotations").
func (c *Client) DoRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.restConfig.Host+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	return resp, nil
}

// DoJSON executes an HTTP request against the core API, optionally marshaling
// body as the JSON request payload, and decodes a JSON response into Resp. Pass
// a nil body for requests with no request body (e.g. GET/DELETE).
//
// Any response status not present in okStatuses is treated as an error via
// HandleErrorResponse.
func DoJSON[Resp any](ctx context.Context, c *Client, method, path string, body any, okStatuses ...int) (Resp, error) {
	return doJSON[Resp](ctx, c, method, path, body, nil, okStatuses)
}

// DoJSONNotFound behaves like DoJSON, but returns notFoundErr instead of the
// generic HandleErrorResponse error when the response status is 404. Callers
// typically pass a sentinel wrapped with request-specific context, e.g.
// fmt.Errorf("%s: %w", id, ErrNotFound), so that errors.Is still matches the
// resource-specific sentinel.
func DoJSONNotFound[Resp any](ctx context.Context, c *Client, method, path string, body any, notFoundErr error, okStatuses ...int) (Resp, error) {
	return doJSON[Resp](ctx, c, method, path, body, notFoundErr, okStatuses)
}

func doJSON[Resp any](ctx context.Context, c *Client, method, path string, body any, notFoundErr error, okStatuses []int) (Resp, error) {
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

// DoStatus executes an HTTP request against the core API, optionally marshaling
// body as the JSON request payload, and checks that the response status is one
// of okStatuses without decoding a response body. Use this for endpoints that
// return no useful body (e.g. DELETE, or actions).
func DoStatus(ctx context.Context, c *Client, method, path string, body any, okStatuses ...int) error {
	return doStatus(ctx, c, method, path, body, nil, okStatuses)
}

// DoStatusNotFound behaves like DoStatus, but returns notFoundErr instead of the
// generic HandleErrorResponse error when the response status is 404.
func DoStatusNotFound(ctx context.Context, c *Client, method, path string, body any, notFoundErr error, okStatuses ...int) error {
	return doStatus(ctx, c, method, path, body, notFoundErr, okStatuses)
}

func doStatus(ctx context.Context, c *Client, method, path string, body any, notFoundErr error, okStatuses []int) error {
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

// HandleErrorResponse uses the shared provider handler to format an error response.
func HandleErrorResponse(resp *http.Response) error {
	return providers.HandleErrorResponse(resp)
}
