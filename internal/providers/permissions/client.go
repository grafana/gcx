package permissions

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

// Client manages folder and dashboard permissions through the Grafana HTTP API.
type Client struct {
	httpClient *http.Client
	host       string
}

func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

func folderPath(uid string) string {
	return fmt.Sprintf("/api/folders/%s/permissions", url.PathEscape(uid))
}

func dashboardPath(uid string) string {
	return fmt.Sprintf("/api/dashboards/uid/%s/permissions", url.PathEscape(uid))
}

func (c *Client) GetFolder(ctx context.Context, uid string) ([]Item, error) {
	return c.get(ctx, folderPath(uid), "folder")
}

func (c *Client) SetFolder(ctx context.Context, uid string, items []Item) error {
	return c.post(ctx, folderPath(uid), items, "folder")
}

func (c *Client) GetDashboard(ctx context.Context, uid string) ([]Item, error) {
	return c.get(ctx, dashboardPath(uid), "dashboard")
}

func (c *Client) SetDashboard(ctx context.Context, uid string, items []Item) error {
	return c.post(ctx, dashboardPath(uid), items, "dashboard")
}

func (c *Client) get(ctx context.Context, path, label string) ([]Item, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s permissions request: %w", label, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s permissions: %w", label, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, handleErrorResponse(resp)
	}

	var items []Item
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, fmt.Errorf("failed to decode %s permissions: %w", label, err)
	}
	return items, nil
}

func (c *Client) post(ctx context.Context, path string, items []Item, label string) error {
	payload, err := json.Marshal(setBody{Items: items})
	if err != nil {
		return fmt.Errorf("failed to encode %s permissions: %w", label, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create %s permissions request: %w", label, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to update %s permissions: %w", label, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return handleErrorResponse(resp)
	}

	// Drain body so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}

	var errResp ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil {
		if msg := errResp.Error; msg != "" {
			return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, msg)
		}
		if msg := errResp.Message; msg != "" {
			return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, msg)
		}
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}
	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
