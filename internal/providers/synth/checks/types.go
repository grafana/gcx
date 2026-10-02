package checks

import (
	"errors"
	"strings"
)

const (
	// APIVersion is the K8s envelope API version for SM Check resources.
	APIVersion = "syntheticmonitoring.ext.grafana.app/v1alpha1"
	// Kind is the K8s kind for SM Check resources.
	Kind = "Check"
)

// Check represents a Synthetic Monitoring check as returned by the SM API.
// Field names match the JSON API — ensures lossless round-trips.
type Check struct {
	ID               int64          `json:"id,omitempty"`
	TenantID         int64          `json:"tenantId,omitempty"`
	Job              string         `json:"job"`
	Target           string         `json:"target"`
	Frequency        int64          `json:"frequency"`
	Offset           int64          `json:"offset,omitempty"`
	Timeout          int64          `json:"timeout"`
	Enabled          bool           `json:"enabled"`
	Labels           []Label        `json:"labels,omitempty"`
	Settings         CheckSettings  `json:"settings"`
	Probes           []int64        `json:"probes"` // probe IDs — only used in API requests
	BasicMetricsOnly bool           `json:"basicMetricsOnly,omitempty"`
	AlertSensitivity string         `json:"alertSensitivity,omitempty"`
	Channels         map[string]any `json:"channels,omitempty"`
	Created          float64        `json:"created,omitempty"`
	Modified         float64        `json:"modified,omitempty"`
}

// CheckSpec is the user-facing representation stored in YAML files.
// Probes are stored as human-readable names, not IDs.
type CheckSpec struct {
	Job              string         `json:"job"`
	Target           string         `json:"target"`
	Frequency        int64          `json:"frequency"`
	Offset           int64          `json:"offset,omitempty"`
	Timeout          int64          `json:"timeout"`
	Enabled          bool           `json:"enabled"`
	Labels           []Label        `json:"labels,omitempty"`
	Settings         CheckSettings  `json:"settings"`
	Probes           []string       `json:"probes"` // probe NAMES in YAML files
	BasicMetricsOnly bool           `json:"basicMetricsOnly,omitempty"`
	AlertSensitivity string         `json:"alertSensitivity,omitempty"`
	Channels         map[string]any `json:"channels,omitempty"`
}

// ValidateRequest is the payload for POST check/validate. It is a CheckSpec
// (probes as names — the server resolves names or IDs) plus the optional ID of
// the check being updated, which lets the server treat a target/job match with
// that check as non-conflicting.
type ValidateRequest struct {
	CheckSpec

	ID int64 `json:"id,omitempty"`
}

// Finding severities returned by POST check/validate.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Finding is one problem reported by POST check/validate. Field is a dot-path
// into the check and is empty for findings about the check as a whole
// (structural validation, quota limits).
type Finding struct {
	Severity string `json:"severity"`
	Field    string `json:"field"`
	Msg      string `json:"msg"`
}

// String renders the finding as "field: msg", or just "msg" for findings about
// the check as a whole.
func (f Finding) String() string {
	if f.Field == "" {
		return f.Msg
	}
	return f.Field + ": " + f.Msg
}

// ValidateResult is the response of POST check/validate.
type ValidateResult struct {
	Valid    bool      `json:"valid"`
	Findings []Finding `json:"findings"`
}

// Error returns nil when the check is valid, otherwise an error listing every
// error-severity finding, one per line. Warning findings never fail validation.
func (r ValidateResult) Error() error {
	var lines []string
	for _, f := range r.Findings {
		if f.Severity == SeverityWarning {
			continue
		}
		lines = append(lines, f.String())
	}

	switch {
	case len(lines) > 0:
		return errors.New(strings.Join(lines, "\n"))
	case !r.Valid:
		return errors.New("server reported the check as invalid")
	default:
		return nil
	}
}

// Label is a key-value pair applied to all metrics and events for a check.
type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CheckSettings holds check-type-specific configuration.
// Only one key is set per check (e.g. "http", "ping", "tcp").
// Using map[string]any preserves all fields without requiring typed structs
// for each of the 9 check type variants.
type CheckSettings map[string]any

// CheckType returns the check type name (e.g. "http", "ping").
func (s CheckSettings) CheckType() string {
	for k := range s {
		return k
	}
	return "unknown"
}

// TenantRemote holds the remote write/query target configuration for a tenant.
type TenantRemote struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
}

// Tenant holds the SM tenant info needed for push operations.
type Tenant struct {
	ID            int64        `json:"id"`
	MetricsRemote TenantRemote `json:"metricsRemote"`
}

// CheckDeleteResponse is returned by DELETE /api/v1/check/delete/{id}.
type CheckDeleteResponse struct {
	Msg     string `json:"msg"`
	CheckID int64  `json:"checkId"`
}

// ProbeRef is a minimal probe representation used for name/ID resolution.
type ProbeRef struct {
	ID   int64
	Name string
}
