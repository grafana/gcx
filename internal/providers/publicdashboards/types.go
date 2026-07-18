package publicdashboards

// PublicDashboard represents a Grafana public dashboard configuration.
//
// A public dashboard has two UIDs: UID is the id of the public dashboard
// itself (identifies THIS pd) and DashboardUID is the parent dashboard.
// The K8s resource name is the PD UID (the leaf id); the parent DashboardUID
// is carried in the spec.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type PublicDashboard struct {
	UID          string `json:"uid,omitempty"`
	DashboardUID string `json:"dashboardUid,omitempty"`
	AccessToken  string `json:"accessToken,omitempty"`
	// The three toggles are pointers with omitempty so a partial update (e.g.
	// `-f patch.json` containing only some fields) omits the unset ones from the
	// PATCH body instead of sending them as false — which would otherwise
	// silently disable the dashboard and its toggles server-side. A nil pointer
	// means "leave unchanged"; a non-nil pointer sends the explicit value.
	IsEnabled            *bool  `json:"isEnabled,omitempty"`
	AnnotationsEnabled   *bool  `json:"annotationsEnabled,omitempty"`
	TimeSelectionEnabled *bool  `json:"timeSelectionEnabled,omitempty"`
	Share                string `json:"share,omitempty"`
}

// GetResourceName returns the public dashboard's own UID — the leaf id that
// identifies this pd uniquely within the stack.
func (pd PublicDashboard) GetResourceName() string {
	return pd.UID
}

// SetResourceName restores the UID from a K8s metadata.name round-trip.
func (pd *PublicDashboard) SetResourceName(name string) {
	pd.UID = name
}

// listResp is the shape of the /api/dashboards/public-dashboards response.
type listResp struct {
	PublicDashboards []PublicDashboard `json:"publicDashboards"`
}
