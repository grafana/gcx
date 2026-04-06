package prometheus

import (
	"strconv"
	"time"
)

// QueryRequest represents a Prometheus query request.
type QueryRequest struct {
	Query string
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// IsRange returns true if this is a range query.
func (r QueryRequest) IsRange() bool {
	return !r.Start.IsZero() && !r.End.IsZero()
}

// QueryResponse represents the response from a Prometheus query.
type QueryResponse struct {
	Status    string     `json:"status"`
	Data      ResultData `json:"data"`
	ErrorType string     `json:"errorType,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// ResultData holds the query result data.
type ResultData struct {
	ResultType string   `json:"resultType"`
	Result     []Sample `json:"result"`
}

// Sample represents a single sample from the query result.
type Sample struct {
	Metric map[string]string `json:"metric"`
	Value  []any             `json:"value,omitempty"`  // [timestamp, value] for instant queries
	Values [][]any           `json:"values,omitempty"` // [[timestamp, value], ...] for range queries
}

// LabelsResponse represents the response from a labels query.
type LabelsResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data"`
}

// MetadataResponse represents the response from a metadata query.
type MetadataResponse struct {
	Status string                     `json:"status"`
	Data   map[string][]MetadataEntry `json:"data"`
}

// MetadataEntry represents metadata for a single metric.
type MetadataEntry struct {
	Type string `json:"type"`
	Help string `json:"help"`
	Unit string `json:"unit,omitempty"`
}

// GrafanaQueryResponse represents the response from Grafana's datasource query API.
type GrafanaQueryResponse struct {
	Results map[string]GrafanaResult `json:"results"`
}

// GrafanaResult represents a single result from a Grafana query.
type GrafanaResult struct {
	Frames      []DataFrame `json:"frames,omitempty"`
	Error       string      `json:"error,omitempty"`
	ErrorSource string      `json:"errorSource,omitempty"`
	Status      int         `json:"status,omitempty"`
}

// DataFrame represents a Grafana data frame.
type DataFrame struct {
	Schema DataFrameSchema `json:"schema"`
	Data   DataFrameData   `json:"data"`
}

// DataFrameSchema describes the structure of a data frame.
type DataFrameSchema struct {
	Name   string  `json:"name,omitempty"`
	Fields []Field `json:"fields,omitempty"`
}

// Field describes a field in a data frame.
type Field struct {
	Name   string            `json:"name,omitempty"`
	Type   string            `json:"type,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// DataFrameData contains the actual data values.
type DataFrameData struct {
	Values [][]any `json:"values,omitempty"`
}

// convertGrafanaResponse converts a Grafana query response to the Prometheus-style format.
func convertGrafanaResponse(grafanaResp *GrafanaQueryResponse) *QueryResponse {
	result := &QueryResponse{
		Status: "success",
		Data: ResultData{
			ResultType: "vector",
			Result:     []Sample{},
		},
	}

	// Get the result for refId "A"
	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok {
		return result
	}

	// Process each frame
	for _, frame := range grafanaResult.Frames {
		if len(frame.Schema.Fields) < 2 || len(frame.Data.Values) < 2 {
			continue
		}

		// Find the time field and value field
		var timeIdx, valueIdx = -1, -1
		var labels map[string]string

		for i, field := range frame.Schema.Fields {
			if field.Type == "time" {
				timeIdx = i
			} else if field.Type == "number" || field.Name == "Value" {
				valueIdx = i
				labels = field.Labels
			}
		}

		if timeIdx == -1 || valueIdx == -1 {
			continue
		}

		timeValues := frame.Data.Values[timeIdx]
		valueValues := frame.Data.Values[valueIdx]

		if len(timeValues) == 0 || len(valueValues) == 0 {
			continue
		}

		sample := Sample{
			Metric: labels,
		}

		// Check if this is a range query (multiple values) or instant query (single value)
		if len(timeValues) > 1 {
			result.Data.ResultType = "matrix"
			sample.Values = make([][]any, len(timeValues))
			for i := range timeValues {
				// Convert milliseconds to seconds for Prometheus compatibility
				ts := toFloat64(timeValues[i]) / 1000.0
				val := toFloat64(valueValues[i])
				sample.Values[i] = []any{ts, formatValue(val)}
			}
		} else {
			ts := toFloat64(timeValues[0]) / 1000.0
			val := toFloat64(valueValues[0])
			sample.Value = []any{ts, formatValue(val)}
		}

		result.Data.Result = append(result.Data.Result, sample)
	}

	return result
}

func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

func formatValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
