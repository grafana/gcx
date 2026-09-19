package kg

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

const statusPath = PluginResourcePath + "/asserts/api-server/v1/stack/status"

// StatusComplete is the Status value indicating the Knowledge Graph has
// finished onboarding and is ready to serve entity/relationship queries.
// Enabled can be true while Status is still e.g. "onboarding" — the graph
// exists but isn't built yet — so Active checks both, mirroring the
// pre-existing checkStackStatus in internal/providers/kg/diagnose.go.
const StatusComplete = "complete"

// Status represents the Knowledge Graph stack status.
type Status struct {
	Status                  string              `json:"status"`
	Enabled                 bool                `json:"enabled"`
	AlertManagerConfigured  bool                `json:"alertManagerConfigured"`
	GraphInstanceCreated    bool                `json:"graphInstanceCreated"`
	UseGrafanaManagedAlerts bool                `json:"useGrafanaManagedAlerts"`
	DisabledTime            *string             `json:"disabledTime,omitempty"`
	Version                 int                 `json:"version"`
	SanityCheckResults      []SanityCheckResult `json:"sanityCheckResults,omitempty"`
}

// SanityCheckResult represents a metric sanity check result (MetricSanityCheckResult).
type SanityCheckResult struct {
	CheckName   string             `json:"checkName"`
	DataPresent bool               `json:"dataPresent"`
	StepResults []SanityStepResult `json:"stepResults,omitempty"`
}

// SanityStepResult represents a single step within a sanity check (MetricSanityCheckStepResult).
type SanityStepResult struct {
	Name         string   `json:"name"`
	Troubleshoot string   `json:"troubleshoot,omitempty"`
	Blockers     []string `json:"blockers,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

// GetStatus retrieves the current Knowledge Graph status.
func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	var status Status
	if err := c.GetJSON(ctx, statusPath, &status); err != nil {
		return nil, fmt.Errorf("kg: get status: %w", err)
	}
	return &status, nil
}

// Active reports whether the Knowledge Graph is installed, enabled, AND
// done onboarding (Status == StatusComplete) for the stack this client
// targets. A definitive "no" is (false, nil): an HTTP 404 from GetStatus's
// underlying request (the asserts plugin route is absent — not installed),
// or a 200 response with Enabled == false or Status != StatusComplete (the
// graph is enabled but still being built). Any other failure — transport
// error, 401/403, 5xx, malformed body — is inconclusive and returned as
// (false, err); callers must not treat that as "not activated".
func (c *Client) Active(ctx context.Context) (bool, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatusCode() == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return status.Enabled && status.Status == StatusComplete, nil
}
