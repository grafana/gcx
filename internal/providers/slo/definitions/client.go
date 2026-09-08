package definitions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/client-go/rest"
)

// ErrNotFound is returned when a requested SLO does not exist (HTTP 404).
var ErrNotFound = errors.New("SLO not found")

const (
	basePath     = "/api/plugins/grafana-slo-app/resources/v1/slo"
	sloByUUIDFmt = basePath + "/%s"
)

// Client is an HTTP client for the Grafana SLO API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// Compile-time guards: the capability seam discovers these interfaces via
// runtime type assertion, so a signature drift would otherwise silently
// demote the verb to errors.ErrUnsupported.
var (
	_ adapter.Lister[Slo]  = (*Client)(nil)
	_ adapter.Getter[Slo]  = (*Client)(nil)
	_ adapter.Creator[Slo] = (*Client)(nil)
	_ adapter.Updater[Slo] = (*Client)(nil)
	_ adapter.Deleter[Slo] = (*Client)(nil)
)

// NewClient creates a new SLO definitions client.
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

// newClientFromDeps builds the SLO definitions client used by the
// declarative adapter.Resource[Slo] registration (see resource_adapter.go).
// Unlike NewClient, it reuses the pre-built ClientDeps.HTTP directly and
// constructs no transport of its own (see docs/architecture/patterns.md §
// Provider ConfigLoader).
func newClientFromDeps(deps adapter.ClientDeps) *Client {
	return &Client{
		restConfig: config.NamespacedRESTConfig{
			Config: rest.Config{Host: deps.BaseURL},
		},
		httpClient: deps.HTTP,
	}
}

// List returns all SLO definitions, truncated to opts.Limit when positive
// (implements adapter.Lister[Slo]).
func (c *Client) List(ctx context.Context, opts adapter.ListOptions) ([]Slo, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, basePath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to list SLOs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, providers.HandleErrorResponse(resp)
	}

	var listResp SLOListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("failed to decode SLO list response: %w", err)
	}

	slos := listResp.SLOs
	if slos == nil {
		slos = []Slo{}
	}

	return adapter.TruncateSlice(slos, opts.Limit), nil
}

// Get returns a single SLO definition by UUID.
func (c *Client) Get(ctx context.Context, uuid string) (*Slo, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf(sloByUUIDFmt, uuid), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get SLO %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, providers.HandleErrorResponse(resp)
	}

	var slo Slo
	if err := json.NewDecoder(resp.Body).Decode(&slo); err != nil {
		return nil, fmt.Errorf("failed to decode SLO response: %w", err)
	}

	return &slo, nil
}

// Create creates a new SLO definition and returns the fully populated
// resource (implements adapter.Creator[Slo]). The create endpoint responds
// with only a UUID, so Create re-fetches the created SLO before returning.
func (c *Client) Create(ctx context.Context, slo *Slo) (*Slo, error) {
	resp, err := c.createRequest(ctx, slo)
	if err != nil {
		return nil, fmt.Errorf("failed to create SLO: %w", err)
	}
	created, err := c.Get(ctx, resp.UUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created SLO %q: %w", resp.UUID, err)
	}
	return created, nil
}

// createRequest issues the raw create HTTP request, returning the server's
// create response (UUID + message only, not the full SLO).
func (c *Client) createRequest(ctx context.Context, slo *Slo) (*SLOCreateResponse, error) {
	body, err := json.Marshal(slo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal SLO: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, basePath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create SLO: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, providers.HandleErrorResponse(resp)
	}

	var createResp SLOCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return nil, fmt.Errorf("failed to decode SLO create response: %w", err)
	}

	return &createResp, nil
}

// Update updates an existing SLO definition and returns the fully populated
// resource, re-fetching it after the update (implements adapter.Updater[Slo]).
func (c *Client) Update(ctx context.Context, name string, slo *Slo) (*Slo, error) {
	if err := c.updateRequest(ctx, name, slo); err != nil {
		return nil, fmt.Errorf("failed to update SLO %q: %w", name, err)
	}
	updated, err := c.Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated SLO %q: %w", name, err)
	}
	return updated, nil
}

// updateRequest issues the raw update HTTP request.
func (c *Client) updateRequest(ctx context.Context, uuid string, slo *Slo) error {
	body, err := json.Marshal(slo)
	if err != nil {
		return fmt.Errorf("failed to marshal SLO: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, fmt.Sprintf(sloByUUIDFmt, uuid), bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to update SLO %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		return providers.HandleErrorResponse(resp)
	}

	return nil
}

// Delete deletes an SLO definition by UUID.
func (c *Client) Delete(ctx context.Context, uuid string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf(sloByUUIDFmt, uuid), nil)
	if err != nil {
		return fmt.Errorf("failed to delete SLO %s: %w", uuid, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return providers.HandleErrorResponse(resp)
	}

	return nil
}

// doRequest builds and executes an HTTP request against the Grafana SLO API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.restConfig.Host+path, body)
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
