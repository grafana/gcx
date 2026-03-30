package appo11y

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers/appo11y/overrides"
	"github.com/grafana/gcx/internal/providers/appo11y/settings"
	k8srest "k8s.io/client-go/rest"
)

const (
	overridesPath = "/api/plugin-proxy/grafana-app-observability-app/overrides"
	settingsPath  = "/api/plugin-proxy/grafana-app-observability-app/provisioned-plugin-settings"
)

// Client is an HTTP client for the Grafana App Observability plugin proxy API.
type Client struct {
	host       string
	httpClient *http.Client
}

// NewClient creates a new App Observability client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := k8srest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		host:       cfg.Host,
		httpClient: httpClient,
	}, nil
}

// GetOverrides retrieves the current metrics generator overrides configuration.
// The ETag from the response is stored on the returned config via SetETag.
func (c *Client) GetOverrides(ctx context.Context) (*overrides.MetricsGeneratorConfig, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, overridesPath, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get overrides: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var cfg overrides.MetricsGeneratorConfig
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode overrides response: %w", err)
	}

	cfg.SetETag(resp.Header.Get("ETag"))

	return &cfg, nil
}

// UpdateOverrides posts an updated metrics generator overrides configuration.
// If etag is non-empty, an If-Match header is sent for optimistic concurrency.
func (c *Client) UpdateOverrides(ctx context.Context, cfg *overrides.MetricsGeneratorConfig, etag string) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal overrides: %w", err)
	}

	var extraHeaders map[string]string
	if etag != "" {
		extraHeaders = map[string]string{"If-Match": etag}
	}

	resp, err := c.doRequest(ctx, http.MethodPost, overridesPath, bytes.NewReader(body), extraHeaders)
	if err != nil {
		return fmt.Errorf("failed to update overrides: %w", err)
	}
	defer resp.Body.Close()

	return checkStatus(resp)
}

// GetSettings retrieves the current App Observability plugin settings.
func (c *Client) GetSettings(ctx context.Context) (*settings.PluginSettings, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, settingsPath, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}
	defer resp.Body.Close()

	if err := checkStatus(resp); err != nil {
		return nil, err
	}

	var s settings.PluginSettings
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("failed to decode settings response: %w", err)
	}

	return &s, nil
}

// UpdateSettings posts an updated App Observability plugin settings configuration.
func (c *Client) UpdateSettings(ctx context.Context, s *settings.PluginSettings) error {
	body, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, settingsPath, bytes.NewReader(body), nil)
	if err != nil {
		return fmt.Errorf("failed to update settings: %w", err)
	}
	defer resp.Body.Close()

	return checkStatus(resp)
}

// doRequest builds and executes an HTTP request against the plugin proxy API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, extraHeaders map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	return resp, nil
}

// checkStatus reads and translates a non-2xx HTTP response into an error.
func checkStatus(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	if resp.StatusCode == http.StatusNotFound {
		return errors.New("Grafana App Observability plugin is not installed or not enabled") //nolint:staticcheck // "Grafana" is a proper noun, capitalization is intentional
	}

	if resp.StatusCode == http.StatusPreconditionFailed {
		return errors.New("concurrent modification conflict: overrides were modified since last read — re-fetch and retry")
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("request failed with status %d (could not read body: %w)", resp.StatusCode, err)
	}

	if len(body) > 0 {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("request failed with status %d", resp.StatusCode)
}
