package definitions

// GetResourceName returns the SLO UUID as the resource identity.
func (s Slo) GetResourceName() string { return s.UUID }

// SetResourceName restores the UUID from a metadata name.
func (s *Slo) SetResourceName(name string) { s.UUID = name }

// Slo represents a Grafana SLO definition.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Slo struct {
	UUID                  string                 `json:"uuid,omitempty"`
	Name                  string                 `json:"name"`
	Description           string                 `json:"description"`
	Query                 Query                  `json:"query"`
	Objectives            []Objective            `json:"objectives"`
	Labels                []Label                `json:"labels,omitempty"`
	Alerting              *Alerting              `json:"alerting,omitempty"`
	DestinationDatasource *DestinationDatasource `json:"destinationDatasource,omitempty"`
	Folder                *Folder                `json:"folder,omitempty"`
	SearchExpression      string                 `json:"searchExpression,omitempty"`
	ReadOnly              *ReadOnly              `json:"readOnly,omitempty"`
}

// Query holds the SLO query configuration.
type Query struct {
	Type             string          `json:"type"`
	Freeform         *FreeformQuery  `json:"freeform,omitempty"`
	Ratio            *RatioQuery     `json:"ratio,omitempty"`
	Threshold        *ThresholdQuery `json:"threshold,omitempty"`
	FailureThreshold *ThresholdQuery `json:"failureThreshold,omitempty"`
	GrafanaQueries   []any           `json:"grafanaQueries,omitempty"`
}

// FreeformQuery is a freeform PromQL query.
type FreeformQuery struct {
	Query string `json:"query"`
}

// RatioQuery defines an SLO using success/total metric ratio.
type RatioQuery struct {
	SuccessMetric MetricDef `json:"successMetric"`
	TotalMetric   MetricDef `json:"totalMetric"`
	GroupByLabels []string  `json:"groupByLabels,omitempty"`
}

// ThresholdQuery defines an SLO using a threshold expression.
type ThresholdQuery struct {
	ThresholdExpression        string    `json:"thresholdExpression,omitempty"`
	FailureThresholdExpression string    `json:"failureThresholdExpression,omitempty"`
	Threshold                  Threshold `json:"threshold"`
	GroupByLabels              []string  `json:"groupByLabels,omitempty"`
}

// MetricDef defines a metric used in ratio queries.
type MetricDef struct {
	PrometheusMetric string `json:"prometheusMetric"`
	Type             string `json:"type,omitempty"`
}

// Threshold defines a threshold value and operator.
type Threshold struct {
	Value    float64 `json:"value"`
	Operator string  `json:"operator"`
}

// Objective defines an SLO objective with a target value and time window.
type Objective struct {
	Value  float64 `json:"value"`
	Window string  `json:"window"`
}

// Label is a key-value pair.
type Label struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// DestinationDatasource specifies where SLO metrics are stored.
type DestinationDatasource struct {
	UID string `json:"uid"`
}

// Folder specifies the folder for the SLO.
type Folder struct {
	UID string `json:"uid"`
}

// Alerting holds the alerting configuration for an SLO.
type Alerting struct {
	Labels          []Label          `json:"labels,omitempty"`
	Annotations     []Label          `json:"annotations,omitempty"`
	FastBurn        *AlertingRule    `json:"fastBurn,omitempty"`
	SlowBurn        *AlertingRule    `json:"slowBurn,omitempty"`
	AdvancedOptions *AdvancedOptions `json:"advancedOptions,omitempty"`
}

// AlertingRule holds labels, annotations, and enrichments for an alerting rule.
type AlertingRule struct {
	Labels      []Label      `json:"labels,omitempty"`
	Annotations []Label      `json:"annotations,omitempty"`
	Enrichments []Enrichment `json:"enrichments,omitempty"`
}

// Enrichment defines an enrichment action triggered when an alert fires.
type Enrichment struct {
	Type string `json:"type"`
}

// AdvancedOptions holds advanced alerting options.
type AdvancedOptions struct {
	MinFailures int `json:"minFailures,omitempty"`
}

// ReadOnly holds server-managed read-only fields.
type ReadOnly struct {
	CreationTimestamp     int64                  `json:"creationTimestamp,omitempty"`
	Status                *Status                `json:"status,omitempty"`
	DrillDownDashboardRef *DashboardRef          `json:"drillDownDashboardRef,omitempty"`
	SourceDatasource      *DestinationDatasource `json:"sourceDatasource,omitempty"`
	Provenance            string                 `json:"provenance,omitempty"`
	ParsesAsRatio         bool                   `json:"parsesAsRatio,omitempty"`
	AllowedActions        []string               `json:"allowedActions,omitempty"`
}

// Status holds the current status of an SLO.
type Status struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

// DashboardRef holds a reference to a dashboard.
type DashboardRef struct {
	UID string `json:"UID"`
}

// SLOListResponse is the response for listing SLOs.
type SLOListResponse struct {
	SLOs []Slo `json:"slos"`
}

// SLOCreateResponse is the response for creating an SLO.
type SLOCreateResponse struct {
	UUID    string `json:"uuid"`
	Message string `json:"message"`
}

// ErrorResponse is the response for an error.
type ErrorResponse struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
}
