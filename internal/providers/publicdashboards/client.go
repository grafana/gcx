package publicdashboards

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

const listPath = "/api/dashboards/public-dashboards"

// Client is a Grafana Public Dashboards API client.
type Client struct {
	httpClient *http.Client
	host       string
}

// NewClient creates a new Public Dashboards client bound to the provided REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

func dashboardPath(dashboardUID string) string {
	return fmt.Sprintf("/api/dashboards/uid/%s/public-dashboards", url.PathEscape(dashboardUID))
}

func dashboardItemPath(dashboardUID, pdUID string) string {
	return fmt.Sprintf("/api/dashboards/uid/%s/public-dashboards/%s", url.PathEscape(dashboardUID), url.PathEscape(pdUID))
}

// List returns all public dashboards in the stack.
func (c *Client) List(ctx context.Context) ([]PublicDashboard, error) {
	var wrapper listResp
	if err := c.do(ctx, http.MethodGet, listPath, nil, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.PublicDashboards, nil
}

// Get returns the public dashboard config for the given parent dashboard UID.
func (c *Client) Get(ctx context.Context, dashboardUID string) (*PublicDashboard, error) {
	var pd PublicDashboard
	if err := c.do(ctx, http.MethodGet, dashboardPath(dashboardUID), nil, &pd); err != nil {
		return nil, err
	}
	return &pd, nil
}

// Create creates a new public dashboard config for the given parent dashboard UID.
func (c *Client) Create(ctx context.Context, dashboardUID string, pd *PublicDashboard) (*PublicDashboard, error) {
	var created PublicDashboard
	if err := c.do(ctx, http.MethodPost, dashboardPath(dashboardUID), pd, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// Update patches an existing public dashboard config.
func (c *Client) Update(ctx context.Context, dashboardUID, pdUID string, pd *PublicDashboard) (*PublicDashboard, error) {
	var updated PublicDashboard
	if err := c.do(ctx, http.MethodPatch, dashboardItemPath(dashboardUID, pdUID), pd, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

// Delete removes a public dashboard config.
func (c *Client) Delete(ctx context.Context, dashboardUID, pdUID string) error {
	return c.do(ctx, http.MethodDelete, dashboardItemPath(dashboardUID, pdUID), nil, nil)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if out != nil {
		req.Header.Set("Accept", "application/json")
	}

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

func handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}

	var errResp errorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Message != "" {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, errResp.Message)
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
