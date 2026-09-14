// Package kg is a typed Go client for the Grafana Knowledge Graph (Asserts)
// API. It takes a caller-supplied *http.Client and base URL: it builds no
// transport, discovers no tokens, and reads no config file. Auth is entirely
// the caller's responsibility.
package kg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/client/internal/httperr"
)

// The Knowledge Graph API is reached through the asserts app plugin's
// resource proxy, so baseURL is the Grafana instance root and the proxy path
// is part of the endpoint.
const (
	assertionsPath = "/api/plugins/grafana-asserts-app/resources/asserts/api-server/v1/assertions"
	llmSummaryPath = assertionsPath + "/llm-summary"
)

// Client is a typed HTTP client for the Grafana Knowledge Graph API.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Knowledge Graph client using the given HTTP client
// and base URL. baseURL is the Grafana instance root (for example
// https://example.grafana.net). The caller owns auth: supply an *http.Client
// whose transport already attaches whatever credentials the target Grafana
// instance requires.
func NewClient(httpClient *http.Client, baseURL string) *Client {
	return &Client{httpClient: httpClient, baseURL: baseURL}
}

// LLMSummary returns an assertions summary for the requested entities. The
// response shape is defined by the backend and passed through as decoded
// JSON rather than a fixed struct.
func (c *Client) LLMSummary(ctx context.Context, req LLMSummaryRequest) (map[string]any, error) {
	var result map[string]any
	if err := c.postJSON(ctx, llmSummaryPath, req, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// postJSON performs a POST request with a JSON body and decodes the response into v.
func (c *Client) postJSON(ctx context.Context, path string, body, v any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return httperr.FromResponse(resp)
	}

	return json.NewDecoder(resp.Body).Decode(v)
}

// doRequest builds and executes an HTTP request against the Knowledge Graph API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
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
