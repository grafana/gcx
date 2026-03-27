package checks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
)

// ErrNotFound is returned when a requested check does not exist (HTTP 404).
var ErrNotFound = errors.New("check not found")

// Client is an HTTP client for the Synthetic Monitoring checks API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new SM checks client.
// baseURL is the SM service root (e.g. "https://synthetic-monitoring-api.grafana.net").
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/") + "/api/v1",
		token:      token,
		httpClient: providers.ExternalHTTPClient(),
	}
}

// List returns all checks for the authenticated tenant.
func (c *Client) List(ctx context.Context) ([]Check, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/check/list", nil)
	if err != nil {
		return nil, fmt.Errorf("listing checks: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var checks []Check
	if err := json.NewDecoder(resp.Body).Decode(&checks); err != nil {
		return nil, fmt.Errorf("decoding check list: %w", err)
	}

	if checks == nil {
		return []Check{}, nil
	}

	return checks, nil
}

// Get returns a single check by ID.
func (c *Client) Get(ctx context.Context, id int64) (*Check, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/check/%d", id), nil)
	if err != nil {
		return nil, fmt.Errorf("getting check %d: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var check Check
	if err := json.NewDecoder(resp.Body).Decode(&check); err != nil {
		return nil, fmt.Errorf("decoding check: %w", err)
	}

	return &check, nil
}

// Create creates a new check. The Check must not have ID or TenantID set.
func (c *Client) Create(ctx context.Context, check Check) (*Check, error) {
	body, err := json.Marshal(check)
	if err != nil {
		return nil, fmt.Errorf("marshalling check: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/check/add", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating check: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var created Check
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("decoding created check: %w", err)
	}

	return &created, nil
}

// Update updates an existing check. The Check must have ID and TenantID set.
func (c *Client) Update(ctx context.Context, check Check) (*Check, error) {
	body, err := json.Marshal(check)
	if err != nil {
		return nil, fmt.Errorf("marshalling check: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/check/update", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("updating check %d: %w", check.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var updated Check
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("decoding updated check: %w", err)
	}

	return &updated, nil
}

// Delete deletes a check by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/check/delete/%d", id), nil)
	if err != nil {
		return fmt.Errorf("deleting check %d: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return smcfg.HandleErrorResponse(resp)
	}

	return nil
}

// GetTenant returns the SM tenant info (used to obtain tenantId for push).
func (c *Client) GetTenant(ctx context.Context) (*Tenant, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/tenant", nil)
	if err != nil {
		return nil, fmt.Errorf("getting tenant: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var tenant Tenant
	if err := json.NewDecoder(resp.Body).Decode(&tenant); err != nil {
		return nil, fmt.Errorf("decoding tenant: %w", err)
	}

	return &tenant, nil
}

// ListProbes returns a minimal list of probes for name/ID resolution.
func (c *Client) ListProbes(ctx context.Context) ([]ProbeRef, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/probe/list", nil)
	if err != nil {
		return nil, fmt.Errorf("listing probes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var raw []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding probe list: %w", err)
	}

	probes := make([]ProbeRef, len(raw))
	for i, p := range raw {
		probes[i] = ProbeRef{ID: p.ID, Name: p.Name}
	}

	return probes, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}
