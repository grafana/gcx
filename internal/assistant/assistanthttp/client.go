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
	"github.com/grafana/gcx/internal/gcxerrors"
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
	return c.DoRequestWithHeaders(ctx, method, path, body, nil)
}

// DoRequestWithHeaders builds and executes an HTTP request with custom headers
// against the Assistant plugin API.
func (c *Client) DoRequestWithHeaders(ctx context.Context, method, path string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.restConfig.Host+pluginBasePath+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		req.Header.Set(name, value)
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
// permission is passed to HandleErrorResponse for permission guidance.
func DoEnvelopeRequest[T any](c *Client, ctx context.Context, method, path string, req any, op, permission string) (*T, error) {
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
		return nil, HandleErrorResponse(resp, permission)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode %s response: %w", op, err)
	}
	return &envelope.Data, nil
}

// ForbiddenError identifies permission failures from the Assistant plugin API.
// It preserves the transport error for status classification and body rendering.
type ForbiddenError struct {
	*gcxerrors.HTTPStatusError

	// Permission is the action required by the calling operation, when known.
	Permission string
}

func (e *ForbiddenError) Unwrap() error { return e.HTTPStatusError }

// HandleErrorResponse reads an error response body and returns a formatted error.
// permission names the calling operation's required action; pass "" when unknown.
func HandleErrorResponse(resp *http.Response, permission string) error {
	statusErr := &gcxerrors.HTTPStatusError{
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Message:     fmt.Sprintf("request failed with status %d", resp.StatusCode),
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		statusErr.Message = fmt.Sprintf("request failed with status %d (could not read body: %v)", resp.StatusCode, err)
		statusErr.Cause = err
	} else if len(body) > 0 {
		statusErr.Message = fmt.Sprintf("request failed with status %d: %s", resp.StatusCode, string(body))
		statusErr.ServerMessage = string(body)
	}
	if resp.StatusCode == http.StatusForbidden {
		return &ForbiddenError{HTTPStatusError: statusErr, Permission: permission}
	}
	return statusErr
}

// FormatTime formats a time for table display, returning "-" for zero values.
func FormatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}
