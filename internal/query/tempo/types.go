package tempo

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
)

// AcceptLLM is the Accept header value for Tempo LLM-friendly responses.
const AcceptLLM = "application/vnd.grafana.llm"

// tagScopes lists the valid Tempo tag scopes.
var tagScopes = []string{"resource", "span", "event", "link", "instrumentation"} //nolint:gochecknoglobals // constant-like lookup table

// ValidateTagScope returns an error if scope is non-empty and not one of the valid scopes.
func ValidateTagScope(scope string) error {
	if scope == "" {
		return nil
	}
	if slices.Contains(tagScopes, scope) {
		return nil
	}
	return fmt.Errorf("invalid tag scope %q: must be one of %v", scope, tagScopes)
}

// SearchRequest represents a Tempo trace search request.
type SearchRequest struct {
	Query string
	Start time.Time
	End   time.Time
	Limit int
}

// SearchResponse represents the response from a Tempo trace search.
type SearchResponse struct {
	Traces []SearchTrace `json:"traces"`
}

// SearchTrace represents a single trace in the search results.
type SearchTrace struct {
	TraceID           string                  `json:"traceID"`
	RootServiceName   string                  `json:"rootServiceName"`
	RootTraceName     string                  `json:"rootTraceName"`
	StartTimeUnixNano string                  `json:"startTimeUnixNano"`
	DurationMs        int                     `json:"durationMs"`
	ServiceStats      map[string]ServiceStats `json:"serviceStats,omitempty"`
}

// ServiceStats holds per-service span and error counts returned in Tempo search
// result metadata.
type ServiceStats struct {
	SpanCount  int `json:"spanCount,omitempty"`
	ErrorCount int `json:"errorCount,omitempty"`
}

// BaselineCandidate is a baseline candidate with structural context for
// comparison against the seed trace. SpanCount and ServiceCount are nil when
// Tempo did not return service statistics for the candidate.
type BaselineCandidate struct {
	TraceID           string `json:"traceID"`
	RootServiceName   string `json:"rootServiceName"`
	RootTraceName     string `json:"rootTraceName"`
	StartTimeUnixNano string `json:"startTimeUnixNano"`
	DurationMs        int    `json:"durationMs"`
	SpanCount         *int   `json:"spanCount,omitempty"`
	ServiceCount      *int   `json:"serviceCount,omitempty"`
}

// BaselineResult is the baseline-candidate list for a seed trace, in the order
// returned by search, including the seed's structural profile for context.
type BaselineResult struct {
	SeedTraceID      string              `json:"seedTraceID"`
	SeedSpanCount    int                 `json:"seedSpanCount"`
	SeedServiceCount int                 `json:"seedServiceCount"`
	Query            string              `json:"query"`
	Candidates       []BaselineCandidate `json:"candidates"`
	ListMeta         *cmdio.ListMeta     `json:"list_meta,omitempty" yaml:"list_meta,omitempty"`
}

// GetTraceRequest represents a request to retrieve a single trace by ID.
type GetTraceRequest struct {
	TraceID   string
	Start     time.Time
	End       time.Time
	LLMFormat bool
}

// GetTraceResponse represents the response from a Tempo get-trace request.
type GetTraceResponse struct {
	Trace   map[string]any `json:"trace,omitempty"`
	Metrics map[string]any `json:"metrics,omitempty"`
	Limits  any            `json:"limits,omitempty"`
	Partial *bool          `json:"partial,omitempty"`
}

// TagsRequest represents a request to list trace tag names.
type TagsRequest struct {
	Scope string
	Query string
}

// TagsResponse represents the response from the Tempo tags API.
type TagsResponse struct {
	Scopes []TagScope `json:"scopes"`
}

// TagScope represents a group of tags within a scope.
type TagScope struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// TagValuesRequest represents a request for values of a specific trace tag.
type TagValuesRequest struct {
	Tag       string
	Scope     string
	Query     string
	LLMFormat bool
}

// TagValuesResponse represents the response from the Tempo tag-values API.
type TagValuesResponse struct {
	TagValues       []TagValue       `json:"-"`
	TagValuesByType map[string][]any `json:"-"`
	Metrics         map[string]any   `json:"-"`
	LLMFormat       bool             `json:"-"`
}

// TagValue represents a typed value for a trace tag.
type TagValue struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type standardTagValuesResponse struct {
	TagValues []TagValue     `json:"tagValues"`
	Metrics   map[string]any `json:"metrics,omitempty"`
}

type llmTagValuesResponse struct {
	TagValues map[string][]any `json:"tagValues"`
	Metrics   map[string]any   `json:"metrics,omitempty"`
}

// MarshalJSON preserves the LLM-friendly tag-values shape when the response
// was requested with AcceptLLM. The default shape remains unchanged.
func (r TagValuesResponse) MarshalJSON() ([]byte, error) {
	if r.LLMFormat {
		return json.Marshal(llmTagValuesResponse{
			TagValues: r.llmTagValues(),
			Metrics:   r.Metrics,
		})
	}

	return json.Marshal(standardTagValuesResponse{
		TagValues: r.TagValues,
		Metrics:   r.Metrics,
	})
}

func decodeStandardTagValuesResponse(data []byte) (*TagValuesResponse, error) {
	var wire standardTagValuesResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	return &TagValuesResponse{
		TagValues: wire.TagValues,
		Metrics:   wire.Metrics,
	}, nil
}

func decodeLLMTagValuesResponse(data []byte) (*TagValuesResponse, error) {
	var wire llmTagValuesResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	return &TagValuesResponse{
		TagValues:       flattenTagValues(wire.TagValues),
		TagValuesByType: wire.TagValues,
		Metrics:         wire.Metrics,
		LLMFormat:       true,
	}, nil
}

func (r TagValuesResponse) llmTagValues() map[string][]any {
	if r.TagValuesByType != nil {
		return r.TagValuesByType
	}
	return groupTagValues(r.TagValues)
}

func groupTagValues(values []TagValue) map[string][]any {
	byType := make(map[string][]any)
	for _, value := range values {
		byType[value.Type] = append(byType[value.Type], value.Value)
	}
	return byType
}

func flattenTagValues(byType map[string][]any) []TagValue {
	if len(byType) == 0 {
		return nil
	}

	types := make([]string, 0, len(byType))
	for typ := range byType {
		types = append(types, typ)
	}
	slices.Sort(types)

	values := make([]TagValue, 0)
	for _, typ := range types {
		for _, value := range byType[typ] {
			values = append(values, TagValue{Type: typ, Value: value})
		}
	}
	return values
}

// DiffRequest represents a request to compare two traces (base vs compare).
// Delta semantics are compare - base (B - A). Start/End are optional; when
// zero they are omitted so the datasource performs a full lookback.
type DiffRequest struct {
	BaseTraceID    string
	CompareTraceID string
	Start          time.Time
	End            time.Time
}

// DiffResponse is the trace-diff patch payload, preserved as-is so no fields
// are dropped while the endpoint remains experimental.
type DiffResponse map[string]any

// diffTarget/diffPayload are the wire shapes for the Tempo trace-diff endpoint.
type diffTarget struct {
	TraceID string `json:"traceId"`
	Start   int64  `json:"start,omitempty"`
	End     int64  `json:"end,omitempty"`
}

type diffPayload struct {
	Base    diffTarget `json:"base"`
	Compare diffTarget `json:"compare"`
}

// MetricsRequest represents a Tempo TraceQL metrics query request.
type MetricsRequest struct {
	Query   string
	Start   time.Time
	End     time.Time
	Step    string
	Instant bool
}

// MetricsResponse represents the response from a Tempo metrics query.
type MetricsResponse struct {
	Series  []MetricsSeries `json:"series"`
	Metrics map[string]any  `json:"metrics,omitempty"`
	Instant bool            `json:"-"`
}

// MetricsSeries represents a single series in a metrics response.
type MetricsSeries struct {
	Labels      []MetricsLabel  `json:"labels"`
	Samples     []MetricsSample `json:"samples,omitempty"`
	TimestampMs string          `json:"timestampMs,omitempty"` // instant query
	Value       *float64        `json:"value,omitempty"`       // instant query
}

// MetricsLabel represents a label key-value pair in a metrics series.
type MetricsLabel struct {
	Key   string         `json:"key"`
	Value map[string]any `json:"value"`
}

// MetricsSample represents a single data point in a metrics time series.
type MetricsSample struct {
	TimestampMs string  `json:"timestampMs"`
	Value       float64 `json:"value"`
}
