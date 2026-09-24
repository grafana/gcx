// Package annotations provides a client and commands for managing Grafana
// annotations via the Grafana HTTP API.
package annotations

import "strconv"

// Annotation represents a Grafana annotation.
//
// Annotations are identified purely by a numeric ID; there is no human-readable
// slug. The ResourceIdentity implementation therefore uses the stringified ID
// as the K8s-style resource name.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Annotation struct {
	ID           int64    `json:"id,omitempty"`
	DashboardUID string   `json:"dashboardUID,omitempty"`
	PanelID      int64    `json:"panelId,omitempty"`
	Time         int64    `json:"time,omitempty"`
	TimeEnd      int64    `json:"timeEnd,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	// Text has omitempty so a partial update omits it rather than sending an
	// empty string that would clear the annotation text server-side.
	Text string `json:"text,omitempty"`
}

// GetResourceName returns the numeric annotation ID as a string.
// When ID is zero (unset), it returns the empty string.
func (a Annotation) GetResourceName() string {
	if a.ID == 0 {
		return ""
	}
	return strconv.FormatInt(a.ID, 10)
}

// SetResourceName parses the given name as an int64 and assigns it to a.ID.
// Parse errors are silently ignored per CONSTITUTION guidance for numeric-ID
// resource types — the existing ID is preserved.
func (a *Annotation) SetResourceName(name string) {
	if id, err := strconv.ParseInt(name, 10, 64); err == nil {
		a.ID = id
	}
}

// AnnotationTag is an annotation tag with its usage count, as returned by
// GET /api/annotations/tags.
type AnnotationTag struct {
	Tag   string `json:"tag"`
	Count int64  `json:"count"`
}

// tagsResponse is the envelope returned by GET /api/annotations/tags.
type tagsResponse struct {
	Result struct {
		Tags []AnnotationTag `json:"tags"`
	} `json:"result"`
}

// MassDeleteRequest is the body for POST /api/annotations/mass-delete. Provide
// either AnnotationID, or a DashboardID/DashboardUID together with PanelID.
type MassDeleteRequest struct {
	DashboardID  int64  `json:"dashboardId,omitempty"`
	PanelID      int64  `json:"panelId,omitempty"`
	AnnotationID int64  `json:"annotationId,omitempty"`
	DashboardUID string `json:"dashboardUID,omitempty"`
}
