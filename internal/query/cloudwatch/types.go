package cloudwatch

import (
	"encoding/json"
	"fmt"
	"time"
)

// QueryRequest represents a CloudWatch metric query request.
type QueryRequest struct {
	Namespace  string
	MetricName string
	Region     string
	Statistic  string
	AccountID  string
	Dimensions map[string]string
	MatchExact bool
	Period     int
	Start      time.Time
	End        time.Time
	IntervalMs int64
}

// Frame represents a single time-series frame from a CloudWatch query result.
type Frame struct {
	Name       string
	Labels     map[string]string
	Timestamps []time.Time
	Values     []*float64
}

// QueryResponse holds the parsed CloudWatch query result.
type QueryResponse struct {
	Frames []Frame
}

// Metric represents a CloudWatch metric.
type Metric struct {
	Name      string
	Namespace string
}

// Account represents an AWS account in cross-account monitoring.
type Account struct {
	ID    string
	Label string
	ARN   string
}

// grafanaQueryResponse is the raw Grafana /api/ds/query (or K8s query API) response.
type grafanaQueryResponse struct {
	Results map[string]grafanaResult `json:"results"`
}

type grafanaResult struct {
	Frames      []dataFrame `json:"frames,omitempty"`
	Error       string      `json:"error,omitempty"`
	ErrorSource string      `json:"errorSource,omitempty"`
	Status      int         `json:"status,omitempty"`
}

type dataFrame struct {
	Schema dataFrameSchema `json:"schema"`
	Data   dataFrameData   `json:"data"`
}

type dataFrameSchema struct {
	Name   string  `json:"name,omitempty"`
	Fields []field `json:"fields,omitempty"`
}

type fieldConfig struct {
	DisplayNameFromDS string `json:"displayNameFromDS,omitempty"`
}

type field struct {
	Name   string            `json:"name,omitempty"`
	Type   string            `json:"type,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	Config *fieldConfig      `json:"config,omitempty"`
}

type dataFrameData struct {
	Values []json.RawMessage `json:"values,omitempty"`
}

// resourceItem is the generic shape returned by CloudWatch resource endpoints.
type resourceItem struct {
	Value json.RawMessage `json:"value"`
}

// ParseQueryResponse converts the raw Grafana response bytes into a QueryResponse.
func ParseQueryResponse(body []byte) (*QueryResponse, error) {
	var raw grafanaQueryResponse
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
		return nil, &queryError{source: "cloudwatch", op: "query", status: status, msg: result.Error, errorSource: result.ErrorSource}
	}

	resp := &QueryResponse{
		Frames: make([]Frame, 0, len(result.Frames)),
	}

	for _, df := range result.Frames {
		frame, ok, err := parseDataFrame(df)
		if err != nil {
			return nil, err
		}
		if ok {
			resp.Frames = append(resp.Frames, frame)
		}
	}

	return resp, nil
}

func parseDataFrame(df dataFrame) (Frame, bool, error) {
	if len(df.Schema.Fields) < 2 || len(df.Data.Values) < 2 {
		return Frame{}, false, nil
	}

	var timeIdx, valueIdx = -1, -1
	var labels map[string]string
	var seriesName string

	for i, f := range df.Schema.Fields {
		if f.Type == "time" {
			timeIdx = i
		} else if f.Type == "number" || f.Name == "Value" {
			valueIdx = i
			labels = f.Labels
			if f.Config != nil && f.Config.DisplayNameFromDS != "" {
				seriesName = f.Config.DisplayNameFromDS
			}
		}
	}

	if timeIdx == -1 || valueIdx == -1 {
		return Frame{}, false, nil
	}

	var tsRaw []any
	if err := json.Unmarshal(df.Data.Values[timeIdx], &tsRaw); err != nil {
		return Frame{}, false, fmt.Errorf("failed to parse timestamps: %w", err)
	}

	var valRaw []any
	if err := json.Unmarshal(df.Data.Values[valueIdx], &valRaw); err != nil {
		return Frame{}, false, fmt.Errorf("failed to parse values: %w", err)
	}

	n := min(len(tsRaw), len(valRaw))

	timestamps := make([]time.Time, n)
	values := make([]*float64, n)

	for i := range n {
		ms := toFloat64(tsRaw[i])
		timestamps[i] = time.UnixMilli(int64(ms)).UTC()

		if valRaw[i] != nil {
			v := toFloat64(valRaw[i])
			values[i] = &v
		}
	}

	return Frame{
		Name:       seriesName,
		Labels:     labels,
		Timestamps: timestamps,
		Values:     values,
	}, true, nil
}

func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

// ParseNamespaces parses the /resources/namespaces response (shape: [{"value":"AWS/EC2"}, ...]).
func ParseNamespaces(body []byte) ([]string, error) {
	var items []resourceItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse namespaces: %w", err)
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		var s string
		if err := json.Unmarshal(item.Value, &s); err != nil {
			continue
		}
		result = append(result, s)
	}
	return result, nil
}

// ParseMetrics parses the /resources/metrics response (shape: [{"value":{"name":"...","namespace":"..."}}, ...]).
func ParseMetrics(body []byte) ([]Metric, error) {
	var items []resourceItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse metrics: %w", err)
	}

	type metricValue struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	}

	result := make([]Metric, 0, len(items))
	for _, item := range items {
		var mv metricValue
		if err := json.Unmarshal(item.Value, &mv); err != nil {
			continue
		}
		result = append(result, Metric(mv))
	}
	return result, nil
}

// ParseDimensionKeys parses the /resources/dimension-keys response (shape: [{"value":"InstanceId"}, ...]).
func ParseDimensionKeys(body []byte) ([]string, error) {
	var items []resourceItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse dimension keys: %w", err)
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		var s string
		if err := json.Unmarshal(item.Value, &s); err != nil {
			continue
		}
		result = append(result, s)
	}
	return result, nil
}

// ParseRegions parses the /resources/regions response (shape: [{"value":{"name":"us-east-1"}}, ...]).
func ParseRegions(body []byte) ([]string, error) {
	var items []resourceItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse regions: %w", err)
	}

	type regionValue struct {
		Name string `json:"name"`
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		var rv regionValue
		if err := json.Unmarshal(item.Value, &rv); err != nil {
			continue
		}
		if rv.Name != "" {
			result = append(result, rv.Name)
		}
	}
	return result, nil
}

// ParseAccounts parses the /resources/accounts response (shape: [{"value":{"id":"...","label":"...","arn":"..."}}, ...]).
func ParseAccounts(body []byte) ([]Account, error) {
	var items []resourceItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse accounts: %w", err)
	}

	type accountValue struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		ARN   string `json:"arn"`
	}

	result := make([]Account, 0, len(items))
	for _, item := range items {
		var av accountValue
		if err := json.Unmarshal(item.Value, &av); err != nil {
			continue
		}
		result = append(result, Account(av))
	}
	return result, nil
}

// queryError is the internal typed error for CloudWatch API errors.
type queryError struct {
	source      string
	op          string
	status      int
	msg         string
	errorSource string
}

func (e *queryError) Error() string {
	if e.errorSource != "" {
		return fmt.Sprintf("cloudwatch %s error (status %d, source %s): %s", e.op, e.status, e.errorSource, e.msg)
	}
	return fmt.Sprintf("cloudwatch %s error (status %d): %s", e.op, e.status, e.msg)
}
