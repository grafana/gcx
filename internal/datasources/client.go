package datasources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

const maxResponseBytes = 10 << 20 // 10 MB

// Datasource holds the fields returned by the legacy Grafana datasource REST API.
type Datasource struct {
	UID             string `json:"uid"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	URL             string `json:"url"`
	Access          string `json:"access"`
	IsDefault       bool   `json:"isDefault"`
	ReadOnly        bool   `json:"readOnly"`
	Database        string `json:"database"`
	BasicAuth       bool   `json:"basicAuth"`
	WithCredentials bool   `json:"withCredentials"`
	JSONData        any    `json:"jsonData"`
}

// Client queries Grafana datasources via the NamespacedRESTConfig
// transport, ensuring OAuth proxy mode and token refresh are respected.
// It mirrors the approach used by internal/query/prometheus and internal/query/loki.
type Client struct {
	host       string
	httpClient *http.Client
}

// NewClient creates a client backed by the given REST config's
// transport (including WrapTransport / RefreshTransport in OAuth proxy mode).
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{
		host:       cfg.Host,
		httpClient: httpClient,
	}, nil
}

// List returns all datasources visible to the authenticated user.
func (c *Client) List(ctx context.Context) ([]*Datasource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+"/api/datasources", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to list datasources: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list datasources: HTTP %d: %s", resp.StatusCode, body)
	}

	var datasources []*Datasource
	if err := json.Unmarshal(body, &datasources); err != nil {
		return nil, fmt.Errorf("failed to parse datasources response: %w", err)
	}

	return datasources, nil
}

// GetByUID returns the datasource with the given UID.
func (c *Client) GetByUID(ctx context.Context, uid string) (*Datasource, error) {
	return c.get(ctx, "/api/datasources/uid/"+url.PathEscape(uid), uid)
}

// GetByName returns the datasource with the given display name.
func (c *Client) GetByName(ctx context.Context, name string) (*Datasource, error) {
	return c.get(ctx, "/api/datasources/name/"+url.PathEscape(name), name)
}

func (c *Client) get(ctx context.Context, path, identifier string) (*Datasource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get datasource: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("datasource %q not found", identifier)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get datasource %q: HTTP %d: %s", identifier, resp.StatusCode, body)
	}

	var ds Datasource
	if err := json.Unmarshal(body, &ds); err != nil {
		return nil, fmt.Errorf("failed to parse datasource response: %w", err)
	}

	return &ds, nil
}
