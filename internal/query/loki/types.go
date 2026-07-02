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
type StreamEntry struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // [[timestamp, line], ...]
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
