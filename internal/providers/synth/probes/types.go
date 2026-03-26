package probes

import "strconv"

// GetResourceName returns the probe ID as a string.
func (p Probe) GetResourceName() string { return strconv.FormatInt(p.ID, 10) }

// SetResourceName restores the probe ID from a string. Parse errors are silently ignored.
func (p *Probe) SetResourceName(name string) { p.ID, _ = strconv.ParseInt(name, 10, 64) }

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
