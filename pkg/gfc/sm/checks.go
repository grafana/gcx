package sm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/pkg/gfc"
)

// ErrCheckNotFound is returned when a requested check does not exist (HTTP 404).
var ErrCheckNotFound = errors.New("check not found")

const (
	checkListPath      = "check/list"
	checkAddPath       = "check/add"
	checkUpdatePath    = "check/update"
	checkByIDPathFmt   = "check/%d"
	checkDeletePathFmt = "check/delete/%d"
	checkAdHocPath     = "check/adhoc"
	tenantPath         = "tenant"
)

// ChecksClient is a typed client for the Synthetic Monitoring checks API.
type ChecksClient struct {
	T gfc.Transport
}

// List returns all checks for the authenticated tenant.
func (c *ChecksClient) List(ctx context.Context) ([]Check, error) {
	status, body, err := c.T.Do(ctx, http.MethodGet, checkListPath, nil)
	if err != nil {
		return nil, fmt.Errorf("listing checks: %w", err)
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
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
func (c *ChecksClient) Get(ctx context.Context, id int64) (*Check, error) {
	status, body, err := c.T.Do(ctx, http.MethodGet, fmt.Sprintf(checkByIDPathFmt, id), nil)
	if err != nil {
		return nil, fmt.Errorf("getting check %d: %w", id, err)
	}
	if status == http.StatusNotFound {
		return nil, ErrCheckNotFound
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
	}
	var check Check
	if err := json.Unmarshal(body, &check); err != nil {
		return nil, fmt.Errorf("decoding check: %w", err)
	}
	return &check, nil
}

// Create creates a new check.
func (c *ChecksClient) Create(ctx context.Context, check Check) (*Check, error) {
	reqBody, err := json.Marshal(check)
	if err != nil {
		return nil, fmt.Errorf("marshalling check: %w", err)
	}
	status, body, err := c.T.Do(ctx, http.MethodPost, checkAddPath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating check: %w", err)
	}
	if status != http.StatusOK && status != http.StatusCreated {
		return nil, gfc.FormatError(status, body)
	}
	var created Check
	if err := json.Unmarshal(body, &created); err != nil {
		return nil, fmt.Errorf("decoding created check: %w", err)
	}
	return &created, nil
}

// Update updates an existing check.
func (c *ChecksClient) Update(ctx context.Context, check Check) (*Check, error) {
	reqBody, err := json.Marshal(check)
	if err != nil {
		return nil, fmt.Errorf("marshalling check: %w", err)
	}
	status, body, err := c.T.Do(ctx, http.MethodPost, checkUpdatePath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("updating check %d: %w", check.ID, err)
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
	}
	var updated Check
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, fmt.Errorf("decoding updated check: %w", err)
	}
	return &updated, nil
}

// Delete deletes a check by ID.
func (c *ChecksClient) Delete(ctx context.Context, id int64) error {
	status, body, err := c.T.Do(ctx, http.MethodDelete, fmt.Sprintf(checkDeletePathFmt, id), nil)
	if err != nil {
		return fmt.Errorf("deleting check %d: %w", id, err)
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return gfc.FormatError(status, body)
	}
	return nil
}

// RunAdhoc submits a one-off check execution against the given probes. The
// check is executed immediately and is never saved.
func (c *ChecksClient) RunAdhoc(ctx context.Context, req AdHocCheckRequest) (*AdHocCheckResponse, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshalling ad-hoc check: %w", err)
	}
	status, body, err := c.T.Do(ctx, http.MethodPost, checkAdHocPath, reqBody)
	if err != nil {
		return nil, fmt.Errorf("running ad-hoc check: %w", err)
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
	}
	var resp AdHocCheckResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding ad-hoc check response: %w", err)
	}
	return &resp, nil
}

// GetTenant returns the SM tenant info.
func (c *ChecksClient) GetTenant(ctx context.Context) (*Tenant, error) {
	status, body, err := c.T.Do(ctx, http.MethodGet, tenantPath, nil)
	if err != nil {
		return nil, fmt.Errorf("getting tenant: %w", err)
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
	}
	var tenant Tenant
	if err := json.Unmarshal(body, &tenant); err != nil {
		return nil, fmt.Errorf("decoding tenant: %w", err)
	}
	return &tenant, nil
}

// ListProbes returns a minimal list of probes for name/ID resolution.
func (c *ChecksClient) ListProbes(ctx context.Context) ([]ProbeRef, error) {
	status, body, err := c.T.Do(ctx, http.MethodGet, "probe/list", nil)
	if err != nil {
		return nil, fmt.Errorf("listing probes: %w", err)
	}
	if status != http.StatusOK {
		return nil, gfc.FormatError(status, body)
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
