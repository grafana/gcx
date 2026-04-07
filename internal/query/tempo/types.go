package tempo

import (
	"fmt"
	"slices"
	"time"
)

// AcceptLLM is the Accept header value for LLM-friendly trace responses.
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
	TraceID           string `json:"traceID"`
	RootServiceName   string `json:"rootServiceName"`
	RootTraceName     string `json:"rootTraceName"`
	StartTimeUnixNano string `json:"startTimeUnixNano"`
	DurationMs        int    `json:"durationMs"`
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
	Tag   string
	Scope string
	Query string
}

// TagValuesResponse represents the response from the Tempo tag-values API.
type TagValuesResponse struct {
	TagValues []TagValue `json:"tagValues"`
}

// TagValue represents a typed value for a trace tag.
type TagValue struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
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
