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
	EntityType                 string            `json:"entityType" yaml:"entityType"`
	PropertyMatchers           []PropertyMatcher `json:"propertyMatchers,omitempty" yaml:"propertyMatchers,omitempty"`
	HavingAssertion            bool              `json:"havingAssertion,omitempty" yaml:"havingAssertion,omitempty"`
	HavingPropagatedAssertions bool              `json:"havingPropagatedAssertions,omitempty" yaml:"havingPropagatedAssertions,omitempty"`
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

// SearchResult is a single search result item.
type SearchResult struct {
	ID                 int               `json:"id,omitempty"`
	Name               string            `json:"name"`
	Type               string            `json:"type"`
	EntityType         string            `json:"entityType,omitempty"`
	Active             bool              `json:"active,omitempty"`
	Scope              map[string]string `json:"scope,omitempty"`
	Properties         map[string]any    `json:"properties,omitempty"`
	Assertion          map[string]any    `json:"assertion,omitempty"`
	ConnectedAssertion map[string]any    `json:"connectedAssertion,omitempty"`
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
// Labels selects the assertion to fetch source metrics for and typically contains
// at minimum "alertname" (the insight ID), "asserts_entity_type" and
// "asserts_entity_name", plus any scope labels (env/namespace/site).
type SourceMetricsRequest struct {
	StartTime int64             `json:"startTime" yaml:"startTime"`
	EndTime   int64             `json:"endTime" yaml:"endTime"`
	Labels    map[string]string `json:"labels" yaml:"labels"`
}

// SourceMetricMatcher is a single PromQL label matcher returned by the
// source-metrics endpoint (e.g. {label:"job", op:"=", value:"asserts/model-builder"}).
type SourceMetricMatcher struct {
	Label string `json:"label"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// SourceMetricsResponse is one entry from POST /v1/assertion/source-metrics:
// the metric name and the label matchers that together identify the underlying
// PromQL series sourcing the assertion.
type SourceMetricsResponse struct {
	MetricName string                `json:"metricName"`
	Labels     []SourceMetricMatcher `json:"labels"`
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

// Rule represents a Knowledge Graph prom rule (matches
// PrometheusRulesDto on the backend). A rule is a named container of
// rule groups, each holding alert and/or recording rules.
//
//nolint:recvcheck // Mixed receivers are intentional for Go generics TypedCRUD compatibility.
type Rule struct {
	Name   string      `json:"name"`
	Groups []RuleGroup `json:"groups,omitempty"`
}

// RuleGroup is a group of related Prometheus rules within a Rule file.
type RuleGroup struct {
	Name     string     `json:"name"`
	Interval string     `json:"interval,omitempty"`
	Rules    []PromRule `json:"rules,omitempty"`
}

// PromRule is a single alert or recording rule within a RuleGroup.
type PromRule struct {
	Record          string            `json:"record,omitempty"`
	Alert           string            `json:"alert,omitempty"`
	Expr            string            `json:"expr,omitempty"`
	Duration        string            `json:"for,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty"`
	Labels          map[string]string `json:"labels,omitempty"`
	DisableInGroups []string          `json:"disableInGroups,omitempty"`
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

// LLMSummaryRequest is the request body for POST /v1/assertions/llm-summary.
type LLMSummaryRequest struct {
	StartTime                                     int64       `json:"startTime"`
	EndTime                                       int64       `json:"endTime"`
	EntityKeys                                    []EntityKey `json:"entityKeys"`
	SuggestionSrcEntities                         []EntityKey `json:"suggestionSrcEntities"`
	AlertCategories                               []string    `json:"alertCategories,omitempty"`
	HideAssertionsOlderThanNHours                 int         `json:"hideAssertionsOlderThanNHours"`
	HideAssertionsPresentMoreThanPercentageOfTime int         `json:"hideAssertionsPresentMoreThanPercentageOfTime"`
	IncludeSuggestions                            bool        `json:"includeSuggestions"`
	IncludeRcaPatterns                            bool        `json:"includeRcaPatterns"`
}

// CypherInsight is a single insight on a Cypher search entity.
type CypherInsight struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Category string `json:"category"`
}

// CypherEntity is an entity returned by the Cypher search endpoint.
type CypherEntity struct {
	Type              string          `json:"type"`
	Name              string          `json:"name"`
	Scope             map[string]any  `json:"scope,omitempty"`
	Properties        map[string]any  `json:"properties,omitempty"`
	Insights          []CypherInsight `json:"insights,omitempty"`
	ConnectedInsights []CypherInsight `json:"connectedInsights,omitempty"`
}

// CypherEdge is a relationship returned by the Cypher search endpoint.
type CypherEdge struct {
	Type             string         `json:"type"`
	SourceName       string         `json:"sourceName"`
	SourceType       string         `json:"sourceType"`
	SourceScope      map[string]any `json:"sourceScope,omitempty"`
	DestinationName  string         `json:"destinationName"`
	DestinationType  string         `json:"destinationType"`
	DestinationScope map[string]any `json:"destinationScope,omitempty"`
}

// CypherSearchRequest is the request body for POST /v1/search/cypher.
type CypherSearchRequest struct {
	CypherQuery   string         `json:"cypherQuery"`
	TimeCriteria  *TimeCriteria  `json:"timeCriteria,omitempty"`
	ScopeCriteria *ScopeCriteria `json:"scopeCriteria,omitempty"`
	PageNum       int            `json:"pageNum,omitempty"`
	WithInsights  bool           `json:"withInsights,omitempty"`
}

// CypherSearchResponse is the response from POST /v1/search/cypher.
type CypherSearchResponse struct {
	Entities []CypherEntity `json:"entities"`
	Edges    []CypherEdge   `json:"edges"`
	PageNum  int            `json:"pageNum"`
	LastPage bool           `json:"lastPage"`
}

// KGMetadataOutput is the structured output from gcx kg metadata.
type KGMetadataOutput struct {
	Schema   *KGSchemaResult          `json:"schema,omitempty"`
	Scopes   map[string][]string      `json:"scopes,omitempty"`
	Logs     []LogDrilldownConfig     `json:"logConfigs,omitempty"`
	Traces   []TraceDrilldownConfig   `json:"traceConfigs,omitempty"`
	Profiles []ProfileDrilldownConfig `json:"profileConfigs,omitempty"`
}
