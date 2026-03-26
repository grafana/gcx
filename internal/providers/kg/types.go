// Package kg provides a client for the Grafana Knowledge Graph (Asserts) API.
package kg

// Status represents the Knowledge Graph status.
type Status struct {
	Status     string `json:"status"`
	Progress   int    `json:"progress"`
	Message    string `json:"message"`
	StatusCode int    `json:"status_code"`
}

// Vendor represents a detected vendor in the metrics.
type Vendor struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// VendorsResponse is the response from the vendors API.
type VendorsResponse struct {
	Vendors []Vendor `json:"vendors"`
}

// DatasetsResponse is the response from the datasets API.
type DatasetsResponse struct {
	Items []DatasetItem `json:"items"`
}

// DatasetItem represents a dataset in the API response.
type DatasetItem struct {
	Name       string `json:"name"`
	Detected   bool   `json:"detected"`
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
}

// DatasetConfig holds configuration for activating a dataset.
type DatasetConfig struct {
	Enabled      bool              `json:"enabled" yaml:"enabled"`
	Matchers     map[string]string `json:"matchers,omitempty" yaml:"matchers,omitempty"`
	FilterGroups []FilterGroup     `json:"filterGroups,omitempty" yaml:"filterGroups,omitempty"`
}

// DatasetActivationRequest is the request body for activating a dataset.
type DatasetActivationRequest struct {
	DatasetType     string        `json:"datasetType"`
	DisabledVendors []string      `json:"disabledVendors"`
	FilterGroups    []FilterGroup `json:"filterGroups"`
}

// FilterGroup defines filtering for dataset activation.
type FilterGroup struct {
	Filters         []string `json:"filters"`
	EnvLabel        string   `json:"envLabel"`
	SiteLabel       string   `json:"siteLabel"`
	EnvLabelValues  []string `json:"envLabelValues"`
	SiteLabelValues []string `json:"siteLabelValues"`
}

// EnvironmentConfig holds environment/logs mapping configuration.
type EnvironmentConfig struct {
	EnvName       string            `json:"envName" yaml:"envName"`
	LokiDSUID     string            `json:"lokiDsUid,omitempty" yaml:"lokiDsUid,omitempty"`
	LogsMapping   map[string]string `json:"logsMapping,omitempty" yaml:"logsMapping,omitempty"`
	CustomMapping map[string]string `json:"customMapping,omitempty" yaml:"customMapping,omitempty"`
}

// ServiceDashboardConfig holds service dashboard configuration.
type ServiceDashboardConfig struct {
	FolderUID   string `json:"folderUid" yaml:"folderUid"`
	FolderTitle string `json:"folderTitle" yaml:"folderTitle"`
}

// KPIDisplayConfig holds configuration for the KPI drawer display settings.
type KPIDisplayConfig struct {
	DefaultDashboard    bool `json:"defaultDashboard" yaml:"default_dashboard"`
	AdditionalDashboard bool `json:"additionalDashboard" yaml:"additional_dashboard"`
	FrameworkDashboard  bool `json:"frameworkDashboard" yaml:"framework_dashboard"`
	RuntimeDashboard    bool `json:"runtimeDashboard" yaml:"runtime_dashboard"`
	K8sAppView          bool `json:"k8sAppView" yaml:"k8s_app_view"`
	AppO11yAppView      bool `json:"appO11yAppView" yaml:"app_o11y_app_view"`
	ProfilesView        bool `json:"profilesView" yaml:"profiles_view"`
	FrontendO11yAppView bool `json:"frontendO11yAppView" yaml:"frontend_o11y_app_view"`
	AWSAppView          bool `json:"awsAppView" yaml:"aws_app_view"`
	LogsView            bool `json:"logsView" yaml:"logs_view"`
	TracesView          bool `json:"tracesView" yaml:"traces_view"`
	PropertiesView      bool `json:"propertiesView" yaml:"properties_view"`
	MetricsView         bool `json:"metricsView" yaml:"metrics_view"`
}

// FrontendO11yRuleGroup represents a group of Frontend O11y recording rules.
type FrontendO11yRuleGroup struct {
	Name  string             `json:"name" yaml:"name"`
	Rules []FrontendO11yRule `json:"rules" yaml:"rules"`
}

// FrontendO11yRule represents a single Frontend O11y recording rule.
type FrontendO11yRule struct {
	Record string            `json:"record" yaml:"record"`
	Expr   string            `json:"expr" yaml:"expr"`
	Labels map[string]string `json:"labels" yaml:"labels"`
}

// EntityKey identifies an entity in the Knowledge Graph.
type EntityKey struct {
	Type  string         `json:"type" yaml:"type"`
	Name  string         `json:"name" yaml:"name"`
	Scope map[string]any `json:"scope,omitempty" yaml:"scope,omitempty"`
}

// EntityCountRequest is the request body for POST /v1/entity_type/count.
type EntityCountRequest struct {
	TimeCriteria     *TimeCriteria     `json:"timeCriteria,omitempty"`
	ScopeCriteria    *ScopeCriteria    `json:"scopeCriteria,omitempty"`
	PropertyMatchers []PropertyMatcher `json:"propertyMatchers,omitempty"`
}

// AssertionsRequest is the request body for POST /v1/assertions.
type AssertionsRequest struct {
	StartTime                     int64       `json:"startTime" yaml:"startTime"`
	EndTime                       int64       `json:"endTime" yaml:"endTime"`
	EntityKeys                    []EntityKey `json:"entityKeys" yaml:"entityKeys"`
	IncludeConnectedAssertions    bool        `json:"includeConnectedAssertions,omitempty" yaml:"includeConnectedAssertions,omitempty"`
	AlertCategories               []string    `json:"alertCategories,omitempty" yaml:"alertCategories,omitempty"`
	Severities                    []string    `json:"severities,omitempty" yaml:"severities,omitempty"`
	HideAssertionsOlderThanNHours int         `json:"hideAssertionsOlderThanNHours,omitempty" yaml:"hideAssertionsOlderThanNHours,omitempty"`
}

// TimeCriteria defines a time range for search queries.
type TimeCriteria struct {
	Instant int64 `json:"instant,omitempty" yaml:"instant,omitempty"`
	Start   int64 `json:"start,omitempty" yaml:"start,omitempty"`
	End     int64 `json:"end,omitempty" yaml:"end,omitempty"`
}

// ScopeCriteria filters by label values.
type ScopeCriteria struct {
	NameAndValues map[string][]string `json:"nameAndValues,omitempty" yaml:"nameAndValues,omitempty"`
}

// PropertyMatcher is a filter on an entity property.
type PropertyMatcher struct {
	ID    int    `json:"id" yaml:"id"`
	Name  string `json:"name" yaml:"name"`
	Op    string `json:"op" yaml:"op"`
	Type  string `json:"type" yaml:"type"`
	Value string `json:"value" yaml:"value"`
}

// EntityMatcher filters entities by type and optional property matchers.
type EntityMatcher struct {
	EntityType       string            `json:"entityType" yaml:"entityType"`
	PropertyMatchers []PropertyMatcher `json:"propertyMatchers,omitempty" yaml:"propertyMatchers,omitempty"`
	HavingAssertion  bool              `json:"havingAssertion,omitempty" yaml:"havingAssertion,omitempty"`
}

// SearchRequest is the request body for POST /v1/search.
type SearchRequest struct {
	DefinitionId   *int              `json:"definitionId,omitempty" yaml:"definitionId,omitempty"`
	TimeCriteria   *TimeCriteria     `json:"timeCriteria,omitempty" yaml:"timeCriteria,omitempty"`
	ScopeCriteria  *ScopeCriteria    `json:"scopeCriteria,omitempty" yaml:"scopeCriteria,omitempty"`
	FilterCriteria []EntityMatcher   `json:"filterCriteria" yaml:"filterCriteria"`
	Bindings       map[string]string `json:"bindings,omitempty" yaml:"bindings,omitempty"`
	PageNum        int               `json:"pageNum" yaml:"pageNum"`
}

// SampleSearchRequest is the request body for POST /v1/search/sample.
type SampleSearchRequest struct {
	TimeCriteria   *TimeCriteria   `json:"timeCriteria,omitempty" yaml:"timeCriteria,omitempty"`
	ScopeCriteria  *ScopeCriteria  `json:"scopeCriteria,omitempty" yaml:"scopeCriteria,omitempty"`
	FilterCriteria []EntityMatcher `json:"filterCriteria" yaml:"filterCriteria"`
	SampleSize     int             `json:"sampleSize" yaml:"sampleSize"`
}

// AssertionScores contains assertion score data from the summary response.
type AssertionScores struct {
	TotalScore              float64            `json:"totalScore"`
	Metrics                 []any              `json:"metrics,omitempty"`
	SeverityWiseTotalScores map[string]float64 `json:"severityWiseTotalScores,omitempty"`
}

// AssertionSummary is the response from POST /v1/assertions/summary.
type AssertionSummary struct {
	Summaries                []any           `json:"summaries,omitempty"`
	TimeWindow               *TimeCriteria   `json:"timeWindow,omitempty"`
	TimeStepIntervalMs       int64           `json:"timeStepIntervalMs,omitempty"`
	AggregateAssertionScores AssertionScores `json:"aggregateAssertionScores"`
}

// SearchResult is a single search result item.
type SearchResult struct {
	ID         int               `json:"id,omitempty"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	EntityType string            `json:"entityType,omitempty"`
	Active     bool              `json:"active,omitempty"`
	Scope      map[string]string `json:"scope,omitempty"`
	Properties map[string]any    `json:"properties,omitempty"`
	Assertion  map[string]any    `json:"assertion,omitempty"`
}

// GraphEntity is the rich entity returned by entity get/lookup endpoints.
type GraphEntity struct {
	ID                   int64                  `json:"id,omitempty"`
	Type                 string                 `json:"type"`
	Name                 string                 `json:"name"`
	Active               bool                   `json:"active,omitempty"`
	Scope                map[string]string      `json:"scope,omitempty"`
	Properties           map[string]any         `json:"properties,omitempty"`
	ConnectedEntityTypes map[string]int         `json:"connectedEntityTypes,omitempty"`
	Assertion            *GraphAssertionSummary `json:"assertion,omitempty"`
	ConnectedAssertion   *GraphAssertionSummary `json:"connectedAssertion,omitempty"`
	AssertionCount       int                    `json:"assertionCount,omitempty"`
}

// GraphAssertionSummary holds assertion info on an entity or its connected entities.
type GraphAssertionSummary struct {
	Severity   string           `json:"severity,omitempty"`
	Amend      bool             `json:"amend,omitempty"`
	Assertions []GraphAssertion `json:"assertions,omitempty"`
}

// GraphAssertion is a single active assertion within a GraphAssertionSummary.
type GraphAssertion struct {
	AssertionName string `json:"assertionName"`
	Severity      string `json:"severity"`
	Category      string `json:"category"`
	EntityType    string `json:"entityType"`
}

// AssertionTimeline is an entity's assertion timeline returned by /search/assertions.
type AssertionTimeline struct {
	Type                        string            `json:"type"`
	Name                        string            `json:"name"`
	Scope                       map[string]string `json:"scope,omitempty"`
	TimeWindow                  *TimeCriteria     `json:"timeWindow,omitempty"`
	AllAssertions               []any             `json:"allAssertions,omitempty"`
	InboundClientErrorsBreached bool              `json:"inboundClientErrorsBreached,omitempty"`
}

// AssertionsGraphData is the nested data object in AssertionsGraphResponse.
type AssertionsGraphData struct {
	PageNum                  int            `json:"pageNum"`
	LastPage                 bool           `json:"lastPage"`
	SearchResultsMaxLimitHit bool           `json:"searchResultsMaxLimitHit"`
	Entities                 []any          `json:"entities"`
	Edges                    []any          `json:"edges"`
	Table                    map[string]any `json:"table,omitempty"`
}

// AssertionsGraphResponse is the response from POST /v1/assertions/graph.
type AssertionsGraphResponse struct {
	Type         string              `json:"type,omitempty"`
	TimeCriteria *TimeCriteria       `json:"timeCriteria,omitempty"`
	Data         AssertionsGraphData `json:"data"`
}

// EntityMetricValue is a single data point in an entity metric series.
type EntityMetricValue struct {
	Time  int64   `json:"time"`
	Value float64 `json:"value"`
}

// EntityMetricSeries is a metric series from the entity-metric endpoint.
type EntityMetricSeries struct {
	Query     string              `json:"query"`
	Name      string              `json:"name"`
	FillZeros bool                `json:"fillZeros"`
	Metric    map[string]string   `json:"metric,omitempty"`
	Values    []EntityMetricValue `json:"values"`
}

// EntityMetricRequest is the request body for POST /v1/assertions/entity-metric.
type EntityMetricRequest struct {
	StartTime             int64             `json:"startTime" yaml:"startTime"`
	EndTime               int64             `json:"endTime" yaml:"endTime"`
	Labels                map[string]string `json:"labels" yaml:"labels"`
	ReferenceForThreshold bool              `json:"referenceForThreshold" yaml:"referenceForThreshold"`
}

// EntityMetricResponse is the response from POST /v1/assertions/entity-metric.
type EntityMetricResponse struct {
	TimeWindow         *TimeCriteria        `json:"timeWindow,omitempty"`
	TimeStepIntervalMs int64                `json:"timeStepIntervalMs,omitempty"`
	Thresholds         []any                `json:"thresholds"`
	Metrics            []EntityMetricSeries `json:"metrics"`
}

// SourceMetricsRequest is the request body for POST /v1/assertion/source-metrics.
type SourceMetricsRequest struct {
	AssertionID string `json:"assertionId" yaml:"assertionId"`
	StartTime   int64  `json:"startTime" yaml:"startTime"`
	EndTime     int64  `json:"endTime" yaml:"endTime"`
}

// SourceMetricsResponse is the response from POST /v1/assertion/source-metrics.
type SourceMetricsResponse struct {
	PromQLQuery   string            `json:"promqlQuery"`
	Labels        map[string]string `json:"labels,omitempty"`
	DataSourceUID string            `json:"dataSourceUid,omitempty"`
}

// GraphDisplayConfig holds entity and edge display settings.
type GraphDisplayConfig struct {
	EntityTypes map[string]EntityTypeDisplayConfig `json:"entities"`
	EdgeTypes   map[string]EdgeTypeDisplayConfig   `json:"edges"`
}

// EntityTypeDisplayConfig holds display settings for an entity type.
type EntityTypeDisplayConfig struct {
	Color string `json:"color"`
	Icon  string `json:"icon"`
	Shape string `json:"shape"`
}

// EdgeTypeDisplayConfig holds display settings for an edge type.
type EdgeTypeDisplayConfig struct {
	Color string `json:"color"`
	Style string `json:"style"`
}

// GetResourceName returns the rule name.
func (r Rule) GetResourceName() string { return r.Name }

// SetResourceName restores the rule name.
func (r *Rule) SetResourceName(name string) { r.Name = name }

// Rule represents a Knowledge Graph prom rule.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Rule struct {
	Name        string            `json:"name"`
	Expr        string            `json:"expr,omitempty"`
	Record      string            `json:"record,omitempty"`
	Alert       string            `json:"alert,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}
