package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
)

// Client is a base HTTP client for the Grafana Fleet Management API.
// All operations use POST (gRPC/Connect style JSON-over-HTTP).
//
// The client carries no credentials of its own. Callers build it with an
// *http.Client whose transport authenticates against the Grafana stack, and a
// baseURL that points at the collector app plugin proxy. The plugin adds the
// Fleet Management credentials and the tenant headers server-side.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Fleet Management base client.
// baseURL must already include the plugin proxy prefix.
// If httpClient is nil, httputils.NewDefaultClient is used.
func NewClient(ctx context.Context, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = httputils.NewDefaultClient(ctx)
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// DoRequest builds and executes a POST request against the Fleet Management API.
// It is exported so that packages composing this client can call the base transport.
func (c *Client) DoRequest(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.DoRequestWithHeaders(ctx, path, body, nil)
}

// DoRequestWithHeaders is like DoRequest but adds extra headers to the request.
func (c *Client) DoRequestWithHeaders(ctx context.Context, path string, body any, headers map[string]string) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("fleet: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("fleet: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fleet: execute request: %w", err)
	}

	return resp, nil
}
