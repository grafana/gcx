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
type Client struct {
	baseURL      string
	instanceID   string
	apiToken     string
	useBasicAuth bool
	httpClient   *http.Client
}

// NewClient creates a new Fleet Management base client.
// When useBasicAuth is true, requests use Basic auth with instanceID:apiToken.
// Otherwise, requests use Bearer token auth.
// If httpClient is nil, httputils.NewDefaultClient is used.
func NewClient(baseURL, instanceID, apiToken string, useBasicAuth bool, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = httputils.NewDefaultClient(context.Background())
	}
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		instanceID:   instanceID,
		apiToken:     apiToken,
		useBasicAuth: useBasicAuth,
		httpClient:   httpClient,
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

	if c.useBasicAuth {
		req.SetBasicAuth(c.instanceID, c.apiToken)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fleet: execute request: %w", err)
	}

	return resp, nil
}
