// Package plugins provides a thin client for Grafana's plugin admin API
// (/api/plugins). It is used to check whether a required datasource plugin is
// installed (via GET /api/plugins/{id}/settings — Grafana has no bare
// /api/plugins/{id} route) and, when permitted, to install it. It reuses the
// NamespacedRESTConfig transport so OAuth proxy mode and token refresh are
// respected, mirroring internal/datasources.
package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"k8s.io/client-go/rest"
)

const (
	maxResponseBytes = 4 << 20 // 4 MB
	pluginsPath      = "/api/plugins/"
)

// ErrNotInstalled is returned by Get when the plugin is not installed (HTTP 404).
var ErrNotInstalled = errors.New("plugin not installed")

// Plugin holds the subset of fields returned by GET /api/plugins/{id}.
type Plugin struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
	Info    struct {
		Version string `json:"version"`
	} `json:"info"`
}

// Client talks to the Grafana plugin admin API.
type Client struct {
	host       string
	httpClient *http.Client
}

// NewClient creates a plugins client backed by the given REST config transport.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{host: cfg.Host, httpClient: httpClient}, nil
}

// Get returns the plugin with the given ID. It returns ErrNotInstalled when the
// plugin is not installed (HTTP 404). Other non-200 responses return an error.
//
// It queries GET /api/plugins/{id}/settings: Grafana has no bare
// /api/plugins/{id} route (that path always 404s), and the settings endpoint is
// the canonical per-plugin lookup — it returns 404 "Plugin not found, no
// installed plugin with that id" when the plugin is absent and 200 with the
// plugin metadata when it is installed.
func (c *Client) Get(ctx context.Context, id string) (*Plugin, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+pluginsPath+url.PathEscape(id)+"/settings", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin %q: %w", id, err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotInstalled
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get plugin %q returned HTTP %d: %s", id, resp.StatusCode, bytes.TrimSpace(body))
	}

	var p Plugin
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("failed to parse plugin response: %w", err)
	}
	return &p, nil
}

// IsInstalled reports whether the plugin is installed.
func (c *Client) IsInstalled(ctx context.Context, id string) (bool, error) {
	_, err := c.Get(ctx, id)
	if errors.Is(err, ErrNotInstalled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Install installs a plugin via POST /api/plugins/{id}/install. version may be
// empty to install the latest available version. Requires plugin-admin
// permissions; the server error is surfaced when not permitted (e.g. an
// Enterprise plugin without a license).
func (c *Client) Install(ctx context.Context, id, version string) error {
	payload, err := json.Marshal(map[string]string{"version": version})
	if err != nil {
		return fmt.Errorf("failed to marshal install request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.host+pluginsPath+url.PathEscape(id)+"/install", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to install plugin %q: %w", id, err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResponseBytes)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("install plugin %q returned HTTP %d: %s", id, resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}
