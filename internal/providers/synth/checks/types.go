package checks

import "github.com/grafana/gcx/pkg/gfc/sm"

const (
	// APIVersion is the K8s envelope API version for SM Check resources.
	APIVersion = "syntheticmonitoring.ext.grafana.app/v1alpha1"
	// Kind is the K8s kind for SM Check resources.
	Kind = "Check"
)

// Library types re-exported for internal use.
type (
	Check              = sm.Check
	Label              = sm.Label
	CheckSettings      = sm.CheckSettings
	Tenant             = sm.Tenant
	TenantRemote       = sm.TenantRemote
	ProbeRef           = sm.ProbeRef
	AdHocCheckRequest  = sm.AdHocCheckRequest
	AdHocCheckResponse = sm.AdHocCheckResponse
)

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
