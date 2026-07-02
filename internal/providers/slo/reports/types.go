package reports

// Report represents a Grafana SLO report.
type Report struct {
	UUID             string           `json:"uuid,omitempty"`
	Name             string           `json:"name"`
	Description      string           `json:"description"`
	TimeSpan         string           `json:"timeSpan"`
	Labels           []Label          `json:"labels,omitempty"`
	ReportDefinition ReportDefinition `json:"reportDefinition"`
}

// ReportDefinition holds the list of SLOs included in a report.
type ReportDefinition struct {
	Slos []ReportSlo `json:"slos"`
}

// ReportSlo is a reference to an SLO within a report.
// Weight is a pointer to accommodate future weighted SLO support.
type ReportSlo struct {
	SloUUID string   `json:"sloUuid"`
	Weight  *float64 `json:"weight,omitempty"`
}

// Label is a key-value pair (redeclared to avoid cross-package import).
type Label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ReportListResponse is the response for listing reports.
type ReportListResponse struct {
	Reports []Report `json:"reports"`
}

// ReportCreateResponse is the response for creating a report.
type ReportCreateResponse struct {
	Message string `json:"message"`
	UUID    string `json:"uuid"`
}
