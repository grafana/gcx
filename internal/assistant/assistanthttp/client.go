package assistanthttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

// pluginBasePath is the Grafana plugin proxy prefix. Callers pass paths that
// include the API-version segment (e.g. "/api/v1/investigations" or
// "/api/v2/investigations/{id}/snapshot") so a single client can talk to both
// the v1 and v2 surfaces.
const pluginBasePath = "/api/plugins/grafana-assistant-app/resources"

// Client is a base HTTP client for the Grafana Assistant plugin API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new Assistant client from a Grafana REST config.
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

// DoRequest builds and executes an HTTP request against the Assistant plugin API.
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

// DoEnvelopeRequest runs an HTTP call against the Assistant plugin API and
// decodes the {"data": T} envelope used by the v2 surface. Pass nil for req
// when the request has no body. Accepts 200 OK or 201 Created.
func DoEnvelopeRequest[T any](c *Client, ctx context.Context, method, path string, req any, op string) (*T, error) {
	var body io.Reader
	if req != nil {
		data, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal %s request: %w", op, err)
		}
		body = bytes.NewReader(data)
	}
	resp, err := c.DoRequest(ctx, method, path, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, HandleErrorResponse(resp)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", op, err)
	}
	return &envelope.Data, nil
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

// FormatTime formats a time for table display, returning "-" for zero values.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

// FormatMillis formats a Unix millisecond timestamp for table display.
func FormatMillis(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}
