package checks

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
	Alerts           []CheckAlert   `json:"alerts,omitempty"`
}

// CheckAlert is a per-check alert rule as returned by the SM datasource when
// /sm/check/list is queried with ?includeAlerts=true, or by /sm/check/{id}/alerts.
// Mirrors CheckAlertPublished in the SM app's types.ts.
type CheckAlert struct {
	Name       string  `json:"name"`
	Threshold  float64 `json:"threshold"`
	Period     string  `json:"period,omitempty"`
	RunbookURL string  `json:"runbookUrl,omitempty"`
	Created    float64 `json:"created,omitempty"`
	Modified   float64 `json:"modified,omitempty"`
	Status     string  `json:"status,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// CheckSpec is the user-facing representation stored in YAML files.
// Probes are stored as human-readable names, not IDs.
type CheckSpec struct {
	Job              string        `json:"job"`
	Target           string        `json:"target"`
	Frequency        int64         `json:"frequency"`
	Offset           int64         `json:"offset,omitempty"`
	Timeout          int64         `json:"timeout"`
	Enabled          bool          `json:"enabled"`
	Labels           []Label       `json:"labels,omitempty"`
	Settings         CheckSettings `json:"settings"`
	Probes           []string      `json:"probes"` // probe NAMES in YAML files
	BasicMetricsOnly bool          `json:"basicMetricsOnly,omitempty"`
	AlertSensitivity string        `json:"alertSensitivity,omitempty"`
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
