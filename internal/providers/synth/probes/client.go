package probes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/synth/smcfg"
)

// Client is an HTTP client for the Synthetic Monitoring probes API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a new SM probes client.
// baseURL is the SM service root (e.g. "https://synthetic-monitoring-api.grafana.net").
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/") + "/api/v1",
		token:      token,
		httpClient: providers.ExternalHTTPClient(),
	}
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
	resp, err := c.doRequest(ctx, http.MethodGet, "/probe/list", nil)
	if err != nil {
		return nil, fmt.Errorf("listing probes: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var probeList []Probe
	if err := json.NewDecoder(resp.Body).Decode(&probeList); err != nil {
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
	body, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshalling probe: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/probe/add", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating probe: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var created CreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
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

	body, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshalling update request: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/probe/update", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("resetting probe token %d: %w", probe.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, smcfg.HandleErrorResponse(resp)
	}

	var updated updateResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		return nil, fmt.Errorf("decoding updated probe: %w", err)
	}

	return &updated.Probe, nil
}

// Delete deletes a probe by ID.
func (c *Client) Delete(ctx context.Context, id int64) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, fmt.Sprintf("/probe/delete/%d", id), nil)
	if err != nil {
		return fmt.Errorf("deleting probe %d: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return smcfg.HandleErrorResponse(resp)
	}

	return nil
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
