package sm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/pkg/gfc"
)

const (
	probeListPath      = "probe/list"
	probeAddPath       = "probe/add"
	probeUpdatePath    = "probe/update"
	probeDeletePathFmt = "probe/delete/%d"
)

// ProbesClient is a typed client for the Synthetic Monitoring probes API.
type ProbesClient struct {
	T gfc.Transport
}

// List returns all probes visible to the authenticated tenant.
func (c *ProbesClient) List(ctx context.Context) ([]Probe, error) {
	status, body, err := c.T.Do(ctx, http.MethodGet, probeListPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing probes: %w", err)
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
	}
	var probes []Probe
	if err := json.Unmarshal(body, &probes); err != nil {
		return nil, fmt.Errorf("decoding probe list: %w", err)
	}
	if probes == nil {
		return []Probe{}, nil
	}
	return probes, nil
}

// Get returns a single probe by ID. The SM API has no single-probe endpoint,
// so this calls List and filters by ID.
func (c *ProbesClient) Get(ctx context.Context, id int64) (*Probe, error) {
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

// Create creates a new private probe.
func (c *ProbesClient) Create(ctx context.Context, probe Probe) (*ProbeCreateResponse, error) {
	reqBody, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshalling probe: %w", err)
	}
	status, body, err := c.T.Do(ctx, http.MethodPost, probeAddPath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating probe: %w", err)
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
	}
	var created ProbeCreateResponse
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decoding created probe: %w", err)
	}
	return &created, nil
}

// ResetToken resets a probe's authentication token.
func (c *ProbesClient) ResetToken(ctx context.Context, probe Probe) (*ProbeResetTokenResponse, error) {
	reqBody, err := json.Marshal(probe)
	if err != nil {
		return nil, fmt.Errorf("marshalling probe: %w", err)
	}
	status, body, err := c.T.Do(ctx, http.MethodPost, probeUpdatePath+"?reset-token=true", reqBody)
	if err != nil {
		return nil, fmt.Errorf("resetting probe token %d: %w", probe.ID, err)
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
	}
	var updated ProbeResetTokenResponse
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, fmt.Errorf("decoding updated probe: %w", err)
	}
	if updated.Token == "" {
		return nil, fmt.Errorf("resetting probe token %d: API response did not contain the new token", probe.ID)
	}
	return &updated, nil
}

// Delete deletes a probe by ID.
func (c *ProbesClient) Delete(ctx context.Context, id int64) error {
	status, body, err := c.T.Do(ctx, http.MethodDelete, fmt.Sprintf(probeDeletePathFmt, id), nil)
	if err != nil {
		return fmt.Errorf("deleting probe %d: %w", id, err)
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return gfc.FormatError(status, body)
	}
	return nil
}
