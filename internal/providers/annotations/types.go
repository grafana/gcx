// Package annotations provides a client and commands for managing Grafana
// annotations via the Grafana HTTP API.
package annotations

// Annotation represents a Grafana annotation.
type Annotation struct {
	ID           int64    `json:"id,omitempty"`
	DashboardUID string   `json:"dashboardUID,omitempty"`
	PanelID      int64    `json:"panelId,omitempty"`
	Time         int64    `json:"time,omitempty"`
	TimeEnd      int64    `json:"timeEnd,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Text         string   `json:"text"`
}

// ErrorResponse is the error response body returned by the Grafana core API.
type ErrorResponse struct {
	Message string `json:"message"`
}
