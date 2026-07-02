package probes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	querysynth "github.com/grafana/gcx/internal/query/synth"
)

// SM API paths, relative to the SM API v1 root. They are forwarded verbatim by
// the datasource-proxy `sm` route and prefixed with /api/v1 on the direct path.
const (
	probeListPath      = "probe/list"
	probeAddPath       = "probe/add"
	probeUpdatePath    = "probe/update"
	probeDeletePathFmt = "probe/delete/%d"
)

// Client is a typed client for the Synthetic Monitoring probes API. It owns
// request shapes and response decoding; the dual-mode SM transport (datasource
// proxy primary, direct SM API fallback) lives in internal/query/synth.
type Client struct {
	t *querysynth.Transport
}

// NewClient creates a probes client over the dual-mode SM transport.
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

// CreateResponse is the API response from creating a probe, containing the
// created probe and its authentication token.
type CreateResponse struct {
	Probe Probe  `json:"probe"`
	Token string `json:"token"`
}

// updateResponse wraps the probe returned from the update endpoint.
type updateResponse struct {
	Probe Probe `json:"probe"`
}

// List returns all probes visible to the authenticated tenant.
func (c *Client) List(ctx context.Context) ([]Probe, error) {
	status, body, err := c.t.Do(ctx, http.MethodGet, probeListPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing probes: %w", err)
	}

	if status != http.StatusOK {
		return nil, providers.FormatError(status, body)
	}

	var probeList []Probe
	if err := json.Unmarshal(body, &probeList); err != nil {
		return nil, fmt.Errorf("decoding probe list: %w", err)
	}

	if probeList == nil {
		return []Probe{}, nil
	}

	return probeList, nil
}

// Create creates a new private probe. The probe is sent as flat JSON.
// The response contains the created probe and its authentication token.
func (c *Client) Create(ctx context.Context, probe Probe) (*CreateResponse, error) {
	reqBody, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshalling probe: %w", err)
	}

	status, body, err := c.t.Do(ctx, http.MethodPost, probeAddPath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating probe: %w", err)
	}

	if status != http.StatusOK {
		return nil, providers.FormatError(status, body)
	}

	var created CreateResponse
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decoding created probe: %w", err)
	}

	return &created, nil
}

// Get returns a single probe by ID. The SM API has no single-probe endpoint,
// so this calls List and filters by ID.
func (c *Client) Get(ctx context.Context, id int64) (*Probe, error) {
	all, err := c.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting probe %d: %w", id, err)
	}

	for i := range all {
		if all[i].ID == id {
			return &all[i], nil
		}
	}

	return nil, fmt.Errorf("probe %d not found", id)
}

// ResetToken updates a probe with resetToken set to true, causing the API
// to issue a new authentication token for the probe.
// The SM update API expects flat JSON: all probe fields at top level plus resetToken.
func (c *Client) ResetToken(ctx context.Context, probe Probe) (*Probe, error) {
	raw, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshalling probe: %w", err)
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("converting probe to map: %w", err)
	}

	m["resetToken"] = true

	reqBody, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshalling update request: %w", err)
	}

	status, body, err := c.t.Do(ctx, http.MethodPost, probeUpdatePath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("resetting probe token %d: %w", probe.ID, err)
	}

	if status != http.StatusOK {
		return nil, providers.FormatError(status, body)
	}

	var updated updateResponse
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, fmt.Errorf("decoding updated probe: %w", err)
	}

	return &updated.Probe, nil
}

// Delete deletes a probe by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	status, body, err := c.t.Do(ctx, http.MethodDelete, fmt.Sprintf(probeDeletePathFmt, id), nil)
	if err != nil {
		return fmt.Errorf("deleting probe %d: %w", id, err)
	}

	if status != http.StatusOK && status != http.StatusNoContent {
		return providers.FormatError(status, body)
	}

	return nil
}
