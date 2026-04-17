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
	UID                  string `json:"uid,omitempty"`
	DashboardUID         string `json:"dashboardUid,omitempty"`
	AccessToken          string `json:"accessToken,omitempty"`
	IsEnabled            bool   `json:"isEnabled"`
	AnnotationsEnabled   bool   `json:"annotationsEnabled"`
	TimeSelectionEnabled bool   `json:"timeSelectionEnabled"`
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

// errorResponse captures Grafana's common error body shape ({"message":"..."}).
type errorResponse struct {
	Message string `json:"message"`
}
