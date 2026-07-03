package cloudwatch

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/queryerror"
)

// QueryRequest represents a CloudWatch metric query request.
//
// Dimensions uses map[string][]string (multi-valued) because that is what
// Grafana's CloudWatch backend, the Explore URL panes parser, and
// mcp-grafana all serialize. CLI callers with single-valued dimensions
// should wrap each value in a single-element slice when building the
// request.
type QueryRequest struct {
	Namespace  string
	MetricName string
	Region     string
	Statistic  string
	AccountID  string
	Dimensions map[string][]string
	MatchExact bool
	Period     string
	Start      time.Time
	End        time.Time
	IntervalMs int64
}

// Frame represents a single time-series frame from a CloudWatch query result.
type Frame struct {
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels,omitempty"`
	Timestamps []time.Time       `json:"timestamps"`
	Values     []*float64        `json:"values"`
}

// QueryResponse holds the parsed CloudWatch query result.
type QueryResponse struct {
	Frames []Frame `json:"frames"`
}

// Metric represents a CloudWatch metric.
type Metric struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Account represents an AWS account in cross-account monitoring.
type Account struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	ARN   string `json:"arn"`
}

// ParseQueryResponse converts the raw Grafana response bytes into a QueryResponse.
func ParseQueryResponse(body []byte) (*QueryResponse, error) {
	var raw dataframe.Response
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse cloudwatch response: %w", err)
	}

	result, ok := raw.Results["A"]
	if !ok {
		return &QueryResponse{}, nil
	}

	if result.Error != "" {
		status := result.Status
		if status == 0 {
			status = 400
		}
		return nil, queryerror.New("cloudwatch", "query", status, result.Error, result.ErrorSource)
	}

	resp := &QueryResponse{
		Frames: make([]Frame, 0, len(result.Frames)),
	}

	for _, df := range result.Frames {
		frame, ok := parseDataFrame(df)
		if ok {
			resp.Frames = append(resp.Frames, frame)
		}
	}

	return resp, nil
}

func parseDataFrame(df dataframe.Frame) (Frame, bool) {
	// Treat schema/data length mismatch as malformed (don't index past Data).
	if len(df.Schema.Fields) != len(df.Data.Values) || len(df.Data.Values) < 2 {
		return Frame{}, false
	}

	var timeIdx, valueIdx = -1, -1
	var labels map[string]string
	var seriesName string

	// Stop at the first time/value pair so labels/name stay attached to that column.
	for i, f := range df.Schema.Fields {
		if f.Type == "time" && timeIdx == -1 {
			timeIdx = i
		} else if (f.Type == "number" || f.Name == "Value") && valueIdx == -1 {
			valueIdx = i
			labels = f.Labels
			if f.Config != nil && f.Config.DisplayNameFromDS != "" {
				seriesName = f.Config.DisplayNameFromDS
			}
		}
		if timeIdx != -1 && valueIdx != -1 {
			break
		}
	}

	if timeIdx == -1 || valueIdx == -1 {
		return Frame{}, false
	}

	tsRaw := df.Data.Values[timeIdx]
	valRaw := df.Data.Values[valueIdx]

	n := min(len(tsRaw), len(valRaw))

	timestamps := make([]time.Time, 0, n)
	values := make([]*float64, 0, n)

	for i := range n {
		ms, ok := toFloat64(tsRaw[i])
		if !ok {
			continue
		}
		timestamps = append(timestamps, time.UnixMilli(int64(ms)).UTC())

		if valRaw[i] == nil {
			// Preserve explicit null as a sparse-metric gap.
			values = append(values, nil)
			continue
		}
		v, ok := toFloat64(valRaw[i])
		if !ok {
			// Drop the row so we don't pair the timestamp with a fabricated zero.
			timestamps = timestamps[:len(timestamps)-1]
			continue
		}
		values = append(values, &v)
	}

	if len(timestamps) == 0 {
		// Drop empty/all-unparseable frames so callers see "no data" rather than a phantom series.
		return Frame{}, false
	}

	return Frame{
		Name:       seriesName,
		Labels:     labels,
		Timestamps: timestamps,
		Values:     values,
	}, true
}

// toFloat64 returns ok=false if v is not a number or a parseable numeric string.
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// ParseNamespaces parses the /resources/namespaces response (shape: [{"value":"AWS/EC2"}, ...]).
func ParseNamespaces(body []byte) ([]string, error) {
	return parseValueStringList(body, "namespaces")
}

// ParseMetrics parses the /resources/metrics response (shape: [{"value":{"name":"...","namespace":"..."}}, ...]).
func ParseMetrics(body []byte) ([]Metric, error) {
	var items []struct {
		Value struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse metrics: %w", err)
	}

	result := make([]Metric, len(items))
	for i, item := range items {
		result[i] = Metric{Name: item.Value.Name, Namespace: item.Value.Namespace}
	}
	return result, nil
}

// ParseDimensionKeys parses the /resources/dimension-keys response (shape: [{"value":"InstanceId"}, ...]).
func ParseDimensionKeys(body []byte) ([]string, error) {
	return parseValueStringList(body, "dimension keys")
}

// parseValueStringList decodes a JSON array of {"value": "<string>"} objects.
// Used for both namespaces and dimension keys (identical wire shape).
func parseValueStringList(body []byte, what string) ([]string, error) {
	var items []struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", what, err)
	}

	result := make([]string, len(items))
	for i, item := range items {
		result[i] = item.Value
	}
	return result, nil
}

// ParseRegions parses the /resources/regions response (shape: [{"value":{"name":"us-east-1"}}, ...]).
// Items with an empty name are dropped (the upstream sometimes returns
// catch-all entries with no name).
func ParseRegions(body []byte) ([]string, error) {
	var items []struct {
		Value struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse regions: %w", err)
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		if item.Value.Name != "" {
			result = append(result, item.Value.Name)
		}
	}
	return result, nil
}

// ParseAccounts parses the /resources/accounts response (shape: [{"value":{"id":"...","label":"...","arn":"..."}}, ...]).
func ParseAccounts(body []byte) ([]Account, error) {
	var items []struct {
		Value struct {
			ID    string `json:"id"`
			Label string `json:"label"`
			ARN   string `json:"arn"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse accounts: %w", err)
	}

	result := make([]Account, len(items))
	for i, item := range items {
		result[i] = Account{ID: item.Value.ID, Label: item.Value.Label, ARN: item.Value.ARN}
	}
	return result, nil
}
