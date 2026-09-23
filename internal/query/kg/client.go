// Package kg is a plain transport/query client for the Grafana Knowledge
// Graph (Asserts) API, following the same pattern as internal/query/prometheus
// and internal/query/tempo: it lives outside any provider so multiple
// providers can depend on it without violating the "no provider imports
// another provider" rule in CONSTITUTION.md.
package kg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// PluginResourcePath is the base path for the Grafana Asserts app's plugin
// resource proxy, which fronts the Knowledge Graph (KG) API.
const PluginResourcePath = "/api/plugins/grafana-asserts-app/resources"

// Client is an HTTP client for the Knowledge Graph (Asserts) API.
type Client struct {
	httpClient *http.Client
	host       string
	namespace  string
}

// NewClient creates a new KG client from the given REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("kg: failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host, namespace: cfg.Namespace}, nil
}

// Host returns the configured stack host this client targets.
func (c *Client) Host() string { return c.host }

// Namespace returns the configured stack namespace this client targets.
func (c *Client) Namespace() string { return c.namespace }

// HTTPClient returns the underlying HTTP client.
func (c *Client) HTTPClient() *http.Client { return c.httpClient }

// GetJSON performs a GET request and decodes the JSON response into v.
func (c *Client) GetJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return fmt.Errorf("kg: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return ReadError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// PostJSON performs a POST request with a JSON body and decodes the response into v.
// If v is nil, the response body is discarded.
func (c *Client) PostJSON(ctx context.Context, path string, body, v any) error {
	return c.DoJSON(ctx, http.MethodPost, path, body, v)
}

// DoJSON performs an HTTP request with a JSON body and decodes the response into v.
func (c *Client) DoJSON(ctx context.Context, method, path string, body, v any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kg: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, bodyReader)
	if err != nil {
		return fmt.Errorf("kg: create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return ReadError(resp)
	}
	if v != nil {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}

// DoJSONStatus performs a JSON request and returns the HTTP status code so
// callers can distinguish 201-created from 200-updated. Decodes into v if non-nil.
func (c *Client) DoJSONStatus(ctx context.Context, method, path string, body, v any) (int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("kg: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("kg: create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return resp.StatusCode, ReadError(resp)
	}
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return resp.StatusCode, fmt.Errorf("kg: decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// DoYAML performs an HTTP request with a YAML body.
func (c *Client) DoYAML(ctx context.Context, method, path, yamlContent string) error {
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, strings.NewReader(yamlContent))
	if err != nil {
		return fmt.Errorf("kg: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-yaml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return ReadError(resp)
	}
	return nil
}

// APIError is a structured error returned by the KG API.
type APIError struct {
	StatusCode int
	message    string // extracted from JSON body, if available
	rawBody    string
}

// NewAPIError constructs an APIError directly, for callers that need to
// synthesize one (e.g. a synthetic 404) without going through ReadError.
func NewAPIError(statusCode int, message string) *APIError {
	return &APIError{StatusCode: statusCode, message: message}
}

func (e *APIError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("kg: request failed with status %d: %s", e.StatusCode, e.message)
	}
	if e.rawBody != "" {
		return fmt.Sprintf("kg: request failed with status %d: %s", e.StatusCode, e.rawBody)
	}
	return fmt.Sprintf("kg: request failed with status %d", e.StatusCode)
}

func (e *APIError) HTTPStatusCode() int {
	return e.StatusCode
}

func (e *APIError) APIServiceName() string {
	return "Knowledge Graph"
}

func (e *APIError) APIUserMessage() string {
	if e.message != "" {
		return e.message
	}
	return e.rawBody
}

// IsServerError returns true for 5xx status codes.
func (e *APIError) IsServerError() bool {
	return e.StatusCode >= 500
}

// ReadError reads the response body and returns a formatted APIError.
func ReadError(resp *http.Response) *APIError {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &APIError{StatusCode: resp.StatusCode}
	}
	apiErr := &APIError{StatusCode: resp.StatusCode}
	if len(body) > 0 {
		// Try to extract a human-readable message from a JSON error body.
		var jsonErr struct {
			Message string `json:"message"`
		}
		if jsonErr2 := json.Unmarshal(body, &jsonErr); jsonErr2 == nil && jsonErr.Message != "" {
			apiErr.message = jsonErr.Message
		} else {
			apiErr.rawBody = string(body)
		}
	}
	return apiErr
}
