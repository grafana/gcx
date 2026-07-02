package checks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	querysynth "github.com/grafana/gcx/internal/query/synth"
)

// ErrNotFound is returned when a requested check does not exist (HTTP 404).
var ErrNotFound = errors.New("check not found")

// SM API paths, relative to the SM API v1 root. They are forwarded verbatim by
// the datasource-proxy `sm` route and prefixed with /api/v1 on the direct path.
const (
	checkListPath      = "check/list"
	checkAddPath       = "check/add"
	checkUpdatePath    = "check/update"
	checkByIDPathFmt   = "check/%d"
	checkDeletePathFmt = "check/delete/%d"
	tenantPath         = "tenant"
	probeListPath      = "probe/list"
)

// Client is a typed client for the Synthetic Monitoring checks API. It owns
// request shapes and response decoding; the dual-mode SM transport (datasource
// proxy primary, direct SM API fallback) lives in internal/query/synth.
type Client struct {
	t *querysynth.Transport
}

// NewClient creates a checks client over the dual-mode SM transport.
//
// When datasourceUID is non-empty, requests go through the Grafana datasource
// proxy built from restCfg (carrying the caller's Grafana credential). On a
// proxy 403 — or when datasourceUID is empty — requests fall back to the direct
// SM API, with credentials resolved lazily via fallback.LoadSMConfig. A nil
// fallback disables the direct path.
func NewClient(restCfg config.NamespacedRESTConfig, datasourceUID string, fallback querysynth.FallbackLoader) (*Client, error) {
	t, err := querysynth.NewTransport(restCfg, datasourceUID, fallback)
	if err != nil {
		return nil, err
	}
	return &Client{t: t}, nil
}

// List returns all checks for the authenticated tenant.
func (c *Client) List(ctx context.Context) ([]Check, error) {
	status, body, err := c.t.Do(ctx, http.MethodGet, checkListPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing checks: %w", err)
	}
	if status != http.StatusOK {
		return nil, providers.FormatError(status, body)
	}

	var checks []Check
	if err := json.Unmarshal(body, &checks); err != nil {
		return nil, fmt.Errorf("decoding check list: %w", err)
	}

	if checks == nil {
		return []Check{}, nil
	}

	return checks, nil
}

// Get returns a single check by ID.
func (c *Client) Get(ctx context.Context, id int64) (*Check, error) {
	status, body, err := c.t.Do(ctx, http.MethodGet, fmt.Sprintf(checkByIDPathFmt, id), nil)
	if err != nil {
		return nil, fmt.Errorf("getting check %d: %w", id, err)
	}

	if status == http.StatusNotFound {
		return nil, ErrNotFound
	}

	if status != http.StatusOK {
		return nil, providers.FormatError(status, body)
	}

	var check Check
	if err := json.Unmarshal(body, &check); err != nil {
		return nil, fmt.Errorf("decoding check: %w", err)
	}

	return &check, nil
}

// Create creates a new check. The Check must not have ID or TenantID set.
func (c *Client) Create(ctx context.Context, check Check) (*Check, error) {
	reqBody, err := json.Marshal(check)
	if err != nil {
		return nil, fmt.Errorf("marshalling check: %w", err)
	}

	status, body, err := c.t.Do(ctx, http.MethodPost, checkAddPath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating check: %w", err)
	}

	if status != http.StatusOK && status != http.StatusCreated {
		return nil, providers.FormatError(status, body)
	}

	var created Check
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decoding created check: %w", err)
	}

	return &created, nil
}

// Update updates an existing check. The Check must have ID and TenantID set.
func (c *Client) Update(ctx context.Context, check Check) (*Check, error) {
	reqBody, err := json.Marshal(check)
	if err != nil {
		return nil, fmt.Errorf("marshalling check: %w", err)
	}

	status, body, err := c.t.Do(ctx, http.MethodPost, checkUpdatePath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("updating check %d: %w", check.ID, err)
	}

	if status != http.StatusOK {
		return nil, providers.FormatError(status, body)
	}

	var updated Check
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, fmt.Errorf("decoding updated check: %w", err)
	}

	return &updated, nil
}

// Delete deletes a check by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	status, body, err := c.t.Do(ctx, http.MethodDelete, fmt.Sprintf(checkDeletePathFmt, id), nil)
	if err != nil {
		return fmt.Errorf("deleting check %d: %w", id, err)
	}

	if status != http.StatusOK && status != http.StatusNoContent {
		return providers.FormatError(status, body)
	}

	return nil
}

// GetTenant returns the SM tenant info (used to obtain tenantId for push).
func (c *Client) GetTenant(ctx context.Context) (*Tenant, error) {
	status, body, err := c.t.Do(ctx, http.MethodGet, tenantPath, nil)
	if err != nil {
		return nil, fmt.Errorf("getting tenant: %w", err)
	}

	if status != http.StatusOK {
		return nil, providers.FormatError(status, body)
	}

	var tenant Tenant
	if err := json.Unmarshal(body, &tenant); err != nil {
		return nil, fmt.Errorf("decoding tenant: %w", err)
	}

	return &tenant, nil
}

// ListProbes returns a minimal list of probes for name/ID resolution.
func (c *Client) ListProbes(ctx context.Context) ([]ProbeRef, error) {
	status, body, err := c.t.Do(ctx, http.MethodGet, probeListPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing probes: %w", err)
	}

	if status != http.StatusOK {
		return nil, providers.FormatError(status, body)
	}

	var raw []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decoding probe list: %w", err)
	}

	probes := make([]ProbeRef, len(raw))
	for i, p := range raw {
		probes[i] = ProbeRef{ID: p.ID, Name: p.Name}
	}

	return probes, nil
}
