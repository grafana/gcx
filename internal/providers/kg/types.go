// Package kg provides a client for the Grafana Knowledge Graph (Asserts) API.
package kg

// Status represents the Knowledge Graph stack status.
type Status struct {
	Status                  string              `json:"status"`
	Enabled                 bool                `json:"enabled"`
	AlertManagerConfigured  bool                `json:"alertManagerConfigured"`
	GraphInstanceCreated    bool                `json:"graphInstanceCreated"`
	UseGrafanaManagedAlerts bool                `json:"useGrafanaManagedAlerts"`
	DisabledTime            *string             `json:"disabledTime,omitempty"`
	Version                 int                 `json:"version"`
	SanityCheckResults      []SanityCheckResult `json:"sanityCheckResults,omitempty"`
}

// SanityCheckResult represents a metric sanity check result (MetricSanityCheckResult).
type SanityCheckResult struct {
	CheckName   string             `json:"checkName"`
	DataPresent bool               `json:"dataPresent"`
	StepResults []SanityStepResult `json:"stepResults,omitempty"`
}

// SanityStepResult represents a single step within a sanity check (MetricSanityCheckStepResult).
type SanityStepResult struct {
	Name         string   `json:"name"`
	Troubleshoot string   `json:"troubleshoot,omitempty"`
	Blockers     []string `json:"blockers,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
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
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
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

// GetResourceName returns the composite "Type--Name" identity for the entity.
func (e GraphEntity) GetResourceName() string { return e.Type + "--" + e.Name }

// SetResourceName sets the entity name (Type is set separately).
func (e *GraphEntity) SetResourceName(name string) { e.Name = name }

// Scope represents a scope dimension with its known values.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Scope struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// GetResourceName returns the scope dimension name.
func (s Scope) GetResourceName() string { return s.Name }

// SetResourceName sets the scope dimension name.
func (s *Scope) SetResourceName(name string) { s.Name = name }

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

// ---------------------------------------------------------------------------
// Schema types
// ---------------------------------------------------------------------------

// GraphSchemaEntity is a node returned by the schema search query.
// Its Name field is the entity type name (e.g. "Service", "Pod"), not an instance name.
type GraphSchemaEntity struct {
	ID         *int64         `json:"id,omitempty"`
	Name       string         `json:"name"`
	Properties map[string]any `json:"properties,omitempty"`
}

// GraphSchemaEdge is a relationship between two entity type nodes in the schema response.
type GraphSchemaEdge struct {
	Source      int64  `json:"source"`
	Destination int64  `json:"destination"`
	Type        string `json:"type"`
}

// GraphSchemaResponse is the raw response from the schema search query (definitionId=6).
type GraphSchemaResponse struct {
	Data struct {
		Entities []GraphSchemaEntity `json:"entities"`
		Edges    []GraphSchemaEdge   `json:"edges"`
	} `json:"data"`
}

// ---------------------------------------------------------------------------
// Telemetry drilldown config types (v2 API)
// ---------------------------------------------------------------------------

// TelemetryConfigMatcher is a single match criterion in a drilldown config.
type TelemetryConfigMatcher struct {
	Property string   `json:"property"`
	Op       string   `json:"op"`
	Values   []string `json:"values,omitempty"`
}

// LogDrilldownConfig maps entity properties to Loki stream selector labels.
type LogDrilldownConfig struct {
	Name                            string                   `json:"name"`
	Match                           []TelemetryConfigMatcher `json:"match"`
	DefaultConfig                   bool                     `json:"defaultConfig"`
	DataSourceUID                   string                   `json:"dataSourceUid"`
	ErrorLabel                      string                   `json:"errorLabel,omitempty"`
	EntityPropertyToLogLabelMapping map[string]string        `json:"entityPropertyToLogLabelMapping"`
	FilterBySpanID                  bool                     `json:"filterBySpanId,omitempty"`
	FilterByTraceID                 bool                     `json:"filterByTraceId,omitempty"`
	Priority                        int                      `json:"priority"`
}

// TraceDrilldownConfig maps entity properties to Tempo trace query labels.
type TraceDrilldownConfig struct {
	Name                              string                   `json:"name"`
	Match                             []TelemetryConfigMatcher `json:"match"`
	DefaultConfig                     bool                     `json:"defaultConfig"`
	DataSourceUID                     string                   `json:"dataSourceUid"`
	EntityPropertyToTraceLabelMapping map[string]string        `json:"entityPropertyToTraceLabelMapping"`
	Priority                          int                      `json:"priority"`
}

// ProfileDrilldownConfig maps entity properties to Pyroscope profile query labels.
type ProfileDrilldownConfig struct {
	Name                                string                   `json:"name"`
	Match                               []TelemetryConfigMatcher `json:"match"`
	DefaultConfig                       bool                     `json:"defaultConfig"`
	DataSourceUID                       string                   `json:"dataSourceUid"`
	EntityPropertyToProfileLabelMapping map[string]string        `json:"entityPropertyToProfileLabelMapping"`
	Priority                            int                      `json:"priority"`
}

// LogConfigsResponse is the response from GET /v2/config/log.
type LogConfigsResponse struct {
	LogDrilldownConfigs []LogDrilldownConfig `json:"logDrilldownConfigs"`
}

// TraceConfigsResponse is the response from GET /v2/config/trace.
type TraceConfigsResponse struct {
	TraceDrilldownConfigs []TraceDrilldownConfig `json:"traceDrilldownConfigs"`
}

// ProfileConfigsResponse is the response from GET /v2/config/profile.
type ProfileConfigsResponse struct {
	ProfileDrilldownConfigs []ProfileDrilldownConfig `json:"profileDrilldownConfigs"`
}

// ---------------------------------------------------------------------------
// Metadata output type
// ---------------------------------------------------------------------------

// EntityTypeSchema is one entity type entry in the formatted schema.
type EntityTypeSchema struct {
	Type       string   `json:"type"`
	Properties []string `json:"properties"`
}

// KGSchemaResult is the processed schema (entity types + relationship strings).
type KGSchemaResult struct {
	EntityTypes   []EntityTypeSchema `json:"entityTypes"`
	Relationships []string           `json:"relationships"`
}

// KGMetadataOutput is the structured output from gcx kg metadata.
type KGMetadataOutput struct {
	Schema   *KGSchemaResult          `json:"schema,omitempty"`
	Scopes   map[string][]string      `json:"scopes,omitempty"`
	Logs     []LogDrilldownConfig     `json:"logConfigs,omitempty"`
	Traces   []TraceDrilldownConfig   `json:"traceConfigs,omitempty"`
	Profiles []ProfileDrilldownConfig `json:"profileConfigs,omitempty"`
}
