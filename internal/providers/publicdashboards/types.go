package publicdashboards

// PublicDashboard represents a Grafana public dashboard configuration.
type PublicDashboard struct {
	UID                  string `json:"uid,omitempty"`
	DashboardUID         string `json:"dashboardUid,omitempty"`
	AccessToken          string `json:"accessToken,omitempty"`
	IsEnabled            bool   `json:"isEnabled"`
	AnnotationsEnabled   bool   `json:"annotationsEnabled"`
	TimeSelectionEnabled bool   `json:"timeSelectionEnabled"`
	Share                string `json:"share,omitempty"`
}

// listResp is the shape of the /api/dashboards/public-dashboards response.
type listResp struct {
	PublicDashboards []PublicDashboard `json:"publicDashboards"`
}

// errorResponse captures Grafana's common error body shape ({"message":"..."}).
type errorResponse struct {
	Message string `json:"message"`
}
