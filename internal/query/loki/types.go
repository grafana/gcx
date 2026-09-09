package loki

import (
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
)

// QueryRequest represents a Loki query request.
type QueryRequest struct {
	Query string
	Start time.Time
	End   time.Time
	Step  time.Duration
	Limit int
}

// IsRange returns true if this is a range query.
func (r QueryRequest) IsRange() bool {
	return !r.Start.IsZero() && !r.End.IsZero()
}

// QueryResponse represents the response from a Loki query.
type QueryResponse struct {
	Status    string          `json:"status"`
	Data      QueryResultData `json:"data"`
	ErrorType string          `json:"errorType,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// QueryResultData holds the query result data.
type QueryResultData struct {
	ResultType string        `json:"resultType"`
	Result     []StreamEntry `json:"result"`
	Stats      *QueryStats   `json:"stats,omitempty"`
	Notices    []FrameNotice `json:"notices,omitempty"`
}

// StreamEntry represents a single log stream from the query result.
//
// Stream holds ONLY the indexed stream labels — the labels that are valid
// inside a {...} LogQL selector. Structured metadata and query-time parsed
// labels are per-line (they can differ between lines that share the same
// indexed labels), so they live on each LogEntry in Values, not here.
type StreamEntry struct {
	Stream map[string]string `json:"stream"`
	Values []LogEntry        `json:"values"`
}

// LogEntry is a single log line within a stream. It carries the line body plus
// the two categories of non-indexed key/value data Loki attaches per line:
//
//   - StructuredMetadata: attached at ingest, not indexed. Only usable as a
//     post-pipe label filter (e.g. `{app="x"} | detected_level="error"`),
//     never inside a {...} selector.
//   - Parsed: extracted at query time by a parser stage (`| json`, `| logfmt`).
//
// Keeping them distinct from the indexed Stream labels lets callers build valid
// LogQL instead of dropping a structured-metadata key into a {...} selector
// (which matches nothing).
type LogEntry struct {
	Timestamp          string            `json:"timestamp"`
	Line               string            `json:"line"`
	StructuredMetadata map[string]string `json:"structuredMetadata,omitempty"`
	Parsed             map[string]string `json:"parsed,omitempty"`
}

// QueryStats contains statistics about the query execution.
type QueryStats struct {
	Summary QuerySummary `json:"summary"`
}

// QuerySummary contains summary statistics.
type QuerySummary struct {
	BytesProcessedPerSecond int64   `json:"bytesProcessedPerSecond,omitempty"`
	LinesProcessedPerSecond int64   `json:"linesProcessedPerSecond,omitempty"`
	TotalBytesProcessed     int64   `json:"totalBytesProcessed,omitempty"`
	TotalLinesProcessed     int64   `json:"totalLinesProcessed,omitempty"`
	ExecTime                float64 `json:"execTime,omitempty"`
}

// LabelsResponse represents the response from the Loki labels API.
type LabelsResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
}

// SeriesResponse represents the response from the Loki series API.
type SeriesResponse struct {
	Status string              `json:"status"`
	Data   []map[string]string `json:"data"`
}

// MetricQueryResponse represents the response from a metric LogQL query.
// It uses the same structure as a Prometheus response (time-series with metric labels).
type MetricQueryResponse struct {
	Status string          `json:"status"`
	Data   MetricQueryData `json:"data"`
}

// MetricQueryData holds the metric query result data.
type MetricQueryData struct {
	ResultType string              `json:"resultType"`
	Result     []MetricQuerySample `json:"result"`
}

// MetricQuerySample represents a single time-series from a metric LogQL query.
type MetricQuerySample struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value,omitempty"`  // [timestamp, value] for instant queries
	Values [][]any           `json:"values,omitempty"` // [[timestamp, value], ...] for range queries
}

// GrafanaQueryResponse is the top-level Grafana datasource query response.
type GrafanaQueryResponse = dataframe.Response

// GrafanaResult represents a single result from a Grafana query.
type GrafanaResult = dataframe.Result

// DataFrame represents a Grafana data frame.
type DataFrame = dataframe.Frame

// DataFrameSchema describes the structure of a data frame.
type DataFrameSchema = dataframe.Schema

// FrameMeta contains metadata about a data frame.
type FrameMeta = dataframe.Meta

// FrameStat represents a single statistic from query execution.
type FrameStat = dataframe.Stat

// FrameNotice represents a notice or warning from the query.
type FrameNotice = dataframe.Notice

// Field describes a field in a data frame.
type Field = dataframe.Field

// DataFrameData contains the actual data values.
type DataFrameData = dataframe.Data
