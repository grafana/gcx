package sm

// Check represents a Synthetic Monitoring check as returned by the SM API.
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
	Probes           []int64        `json:"probes"`
	BasicMetricsOnly bool           `json:"basicMetricsOnly,omitempty"`
	AlertSensitivity string         `json:"alertSensitivity,omitempty"`
	Channels         map[string]any `json:"channels,omitempty"`
	Created          float64        `json:"created,omitempty"`
	Modified         float64        `json:"modified,omitempty"`
}

// Label is a key-value pair applied to all metrics and events for a check.
type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CheckSettings holds check-type-specific configuration.
// Only one key is set per check (e.g. "http", "ping", "tcp").
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

// AdHocCheckRequest is the payload for POST check/adhoc.
type AdHocCheckRequest struct {
	Timeout  int64         `json:"timeout"`
	Settings CheckSettings `json:"settings"`
	Probes   []int64       `json:"probes"`
	Target   string        `json:"target"`
}

// AdHocCheckResponse is returned by POST check/adhoc.
type AdHocCheckResponse struct {
	ID       string        `json:"id"`
	TenantID int64         `json:"tenantId"`
	Timeout  int64         `json:"timeout"`
	Settings CheckSettings `json:"settings"`
	Probes   []int64       `json:"probes"`
	Target   string        `json:"target"`
}

// ProbeRef is a minimal probe representation used for name/ID resolution.
type ProbeRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Probe represents a Synthetic Monitoring probe node.
type Probe struct {
	ID           int64             `json:"id"`
	TenantID     int64             `json:"tenantId"`
	Name         string            `json:"name"`
	Latitude     float64           `json:"latitude"`
	Longitude    float64           `json:"longitude"`
	Labels       []ProbeLabel      `json:"labels,omitempty"`
	Region       string            `json:"region"`
	Public       bool              `json:"public"`
	Online       bool              `json:"online"`
	OnlineChange float64           `json:"onlineChange"`
	Version      string            `json:"version"`
	Deprecated   bool              `json:"deprecated"`
	Created      float64           `json:"created"`
	Modified     float64           `json:"modified"`
	Capabilities ProbeCapabilities `json:"capabilities"`
}

// ProbeLabel is a key-value pair attached to a probe.
type ProbeLabel struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ProbeCapabilities describes what a probe can and cannot run.
type ProbeCapabilities struct {
	DisableScriptedChecks bool `json:"disableScriptedChecks"`
	DisableBrowserChecks  bool `json:"disableBrowserChecks"`
}

// ProbeCreateResponse is the API response from creating a probe.
type ProbeCreateResponse struct {
	Probe Probe  `json:"probe"`
	Token string `json:"token"`
}

// ProbeResetTokenResponse is the API response from resetting a probe token.
type ProbeResetTokenResponse struct {
	Probe Probe  `json:"probe"`
	Token string `json:"token"`
}

// CheckStatusResult holds merged check + metric data for a single check.
type CheckStatusResult struct {
	ID          int64    `json:"id"`
	Job         string   `json:"job"`
	Target      string   `json:"target"`
	Type        string   `json:"type"`
	Success     *float64 `json:"success,omitempty"`
	ProbesUp    int      `json:"probesUp"`
	ProbesTotal int      `json:"probesTotal"`
	LatencyMs   *float64 `json:"latencyMs,omitempty"`
	ProbeNames  []string `json:"probeNames,omitempty"`
	Status      string   `json:"status"`
}

// TimelineSeries holds time series data for a single probe.
type TimelineSeries struct {
	Probe  string
	Points []TimelinePoint
}

// TimelinePoint represents a single data point in the timeline.
type TimelinePoint struct {
	Timestamp float64
	Value     float64
}
