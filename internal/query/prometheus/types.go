package prometheus

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
)

// QueryRequest represents a Prometheus query request.
type QueryRequest struct {
	Query   string
	Start   time.Time
	End     time.Time
	Step    time.Duration
	Instant bool // force instant query even when Start is set
}

// IsRange returns true if this is a range query (both Start and End set, not forced instant).
func (r QueryRequest) IsRange() bool {
	return !r.Instant && !r.Start.IsZero() && !r.End.IsZero()
}

// Validate allows commands to reject invalid ranges before loading credentials.
func (r QueryRequest) Validate() error {
	_, err := r.maxDataPoints()
	return err
}

func (r QueryRequest) maxDataPoints() (int64, error) {
	if !r.IsRange() || r.Step == 0 {
		return 0, nil
	}
	if r.Step < time.Millisecond {
		return 0, fmt.Errorf("query step %s must be at least 1ms", r.Step)
	}
	start, end := r.Start.UnixMilli(), r.End.UnixMilli()
	if end < start || (start < 0 && end > math.MaxInt64+start) {
		return 0, errors.New("query range must be non-negative and fit in int64 milliseconds")
	}
	rangeMs, stepMs := end-start, r.Step.Milliseconds()
	required := rangeMs / stepMs
	if rangeMs%stepMs != 0 {
		required++
	}
	return required, nil
}

// QueryResponse represents the response from a Prometheus query.
type QueryResponse struct {
	Status     string           `json:"status"`
	Data       ResultData       `json:"data"`
	ErrorType  string           `json:"errorType,omitempty"`
	Error      string           `json:"error,omitempty"`
	Resolution *QueryResolution `json:"resolution,omitempty"`
}

// QueryResolution reports timestamp spacing, which can also reflect gaps in data.
type QueryResolution struct {
	RequestedStepMs int64   `json:"requestedStepMs"`
	ObservedStepMs  []int64 `json:"observedStepMs"`
}

func (r *QueryResponse) setResolution(step time.Duration) {
	observed := make(map[int64]struct{})
	for _, sample := range r.Data.Result {
		for i := 1; i < len(sample.Values); i++ {
			previous := toFloat64(sample.Values[i-1][0])
			current := toFloat64(sample.Values[i][0])
			observed[int64(math.Round((current-previous)*1000))] = struct{}{}
		}
	}
	steps := make([]int64, 0, len(observed))
	for value := range observed {
		steps = append(steps, value)
	}
	slices.Sort(steps)
	r.Resolution = &QueryResolution{RequestedStepMs: step.Milliseconds(), ObservedStepMs: steps}
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

// GrafanaQueryResponse is the top-level Grafana datasource query response.
type GrafanaQueryResponse = dataframe.Response

// GrafanaResult represents a single result from a Grafana query.
type GrafanaResult = dataframe.Result

// DataFrame represents a Grafana data frame.
type DataFrame = dataframe.Frame

// DataFrameSchema describes the structure of a data frame.
type DataFrameSchema = dataframe.Schema

// Field describes a field in a data frame.
type Field = dataframe.Field

// DataFrameData contains the actual data values.
type DataFrameData = dataframe.Data

// convertGrafanaResponse converts a Grafana query response to the Prometheus-style
// format. The result type reflects the request intent, not the shape of the data
// that came back: a range query is always a matrix (matching the Prometheus
// query_range contract, even when empty), an instant query a vector. Deriving it
// from the data would make an empty range indistinguishable from an empty instant.
func convertGrafanaResponse(grafanaResp *GrafanaQueryResponse, isRange bool) *QueryResponse {
	resultType := "vector"
	if isRange {
		resultType = "matrix"
	}
	result := &QueryResponse{
		Status: "success",
		Data: ResultData{
			ResultType: resultType,
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

		// Shape the sample by request intent, not point count: a range sample
		// always uses Values (even a single point), so a one-point range is not
		// mistyped as an instant value.
		if isRange {
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
