package azuremonitor

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/queryerror"
)

// QueryRequest represents an Azure Monitor metrics query request.
//
// DimensionFilters maps a dimension name to a single filter value. A value of
// "*" splits the result into one series per dimension value, matching the
// wildcard behavior of the Grafana Azure Monitor query editor.
type QueryRequest struct {
	Subscription     string
	ResourceGroup    string
	ResourceName     string
	MetricNamespace  string
	MetricName       string
	Aggregation      string
	TimeGrain        string
	Region           string
	Top              string
	DimensionFilters map[string]string
	Start            time.Time
	End              time.Time
}

// LogsQueryRequest represents an Azure Log Analytics KQL query request.
type LogsQueryRequest struct {
	Subscription  string
	ResourceGroup string
	Workspace     string
	Query         string
	Start         time.Time
	End           time.Time
}

// ResourceGraphRequest represents an Azure Resource Graph KQL query request.
type ResourceGraphRequest struct {
	Subscriptions []string
	Query         string
	Start         time.Time
	End           time.Time
}

// TableResponse holds a tabular query result (Log Analytics or Resource Graph).
type TableResponse struct {
	Columns []Column `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// Column describes a single column in a tabular query result.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Frame represents a single time-series frame from an Azure Monitor query result.
type Frame struct {
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels,omitempty"`
	Unit       string            `json:"unit,omitempty"`
	Timestamps []time.Time       `json:"timestamps"`
	Values     []*float64        `json:"values"`
}

// QueryResponse holds the parsed Azure Monitor query result.
type QueryResponse struct {
	Frames []Frame `json:"frames"`
}

// Subscription represents an Azure subscription.
type Subscription struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ResourceGroup represents an Azure resource group.
type ResourceGroup struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

// Resource represents an Azure resource.
type Resource struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Location string `json:"location"`
}

// MetricDefinition represents an Azure Monitor metric definition.
type MetricDefinition struct {
	Name                  string   `json:"name"`
	DisplayName           string   `json:"displayName,omitempty"`
	PrimaryAggregation    string   `json:"primaryAggregation"`
	SupportedAggregations []string `json:"supportedAggregations,omitempty"`
	Unit                  string   `json:"unit"`
	Dimensions            []string `json:"dimensions,omitempty"`
}

// ParseQueryResponse converts the raw Grafana response bytes into a QueryResponse.
func ParseQueryResponse(body []byte) (*QueryResponse, error) {
	var raw dataframe.Response
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse azuremonitor response: %w", err)
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
		return nil, queryerror.New("azuremonitor", "query", status, simplifyPluginError(result.Error), result.ErrorSource)
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
	var seriesName, unit string

	// Stop at the first time/value pair so labels/name stay attached to that column.
	for i, f := range df.Schema.Fields {
		switch {
		case f.Type == "time" && timeIdx == -1:
			timeIdx = i
		case f.Type == "number" && valueIdx == -1:
			valueIdx = i
			labels = f.Labels
			seriesName, unit = fieldDisplayMeta(f)
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
		Unit:       unit,
		Timestamps: timestamps,
		Values:     values,
	}, true
}

// ParseTableResponse converts raw Grafana response bytes into a TableResponse.
// All frames sharing the first frame's column layout are appended; time-typed
// columns are converted from epoch milliseconds to RFC3339 strings.
func ParseTableResponse(body []byte, operation string) (*TableResponse, error) {
	var raw dataframe.Response
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse azuremonitor response: %w", err)
	}

	result, ok := raw.Results["A"]
	if !ok {
		return &TableResponse{Columns: []Column{}, Rows: [][]any{}}, nil
	}

	if result.Error != "" {
		status := result.Status
		if status == 0 {
			status = 400
		}
		return nil, queryerror.New("azuremonitor", operation, status, simplifyPluginError(result.Error), result.ErrorSource)
	}

	resp := &TableResponse{Columns: []Column{}, Rows: [][]any{}}
	for _, frame := range result.Frames {
		if len(frame.Schema.Fields) == 0 {
			continue
		}
		if len(resp.Columns) == 0 {
			for _, f := range frame.Schema.Fields {
				resp.Columns = append(resp.Columns, Column{Name: f.Name, Type: f.Type})
			}
		} else if !sameColumns(resp.Columns, frame.Schema.Fields) {
			continue
		}
		appendFrameRows(resp, frame)
	}
	return resp, nil
}

func sameColumns(cols []Column, fields []dataframe.Field) bool {
	if len(cols) != len(fields) {
		return false
	}
	for i, f := range fields {
		if cols[i].Name != f.Name || cols[i].Type != f.Type {
			return false
		}
	}
	return true
}

func appendFrameRows(resp *TableResponse, frame dataframe.Frame) {
	if len(frame.Data.Values) != len(resp.Columns) || len(frame.Data.Values) == 0 {
		return
	}
	numRows := len(frame.Data.Values[0])
	for i := range numRows {
		row := make([]any, len(resp.Columns))
		for colIdx, colValues := range frame.Data.Values {
			if i >= len(colValues) {
				continue
			}
			v := colValues[i]
			if resp.Columns[colIdx].Type == "time" && v != nil {
				if ms, ok := toFloat64(v); ok {
					v = time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339)
				}
			}
			row[colIdx] = v
		}
		resp.Rows = append(resp.Rows, row)
	}
}

// azureAPIError mirrors the nested error envelope returned by Azure APIs:
// {"error": {"code", "message", "innererror": {"code", "message", ...}}}.
type azureAPIError struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	InnerError *azureAPIError `json:"innererror"`
}

// simplifyPluginError extracts the most specific Azure error from the plugin's
// wrapped error string ("request failed, status: ..., body: {...}"), walking
// the innererror chain to the deepest message. Both Azure error body shapes
// are handled: the Log Analytics/ARM envelope {"error": {code, message, ...}}
// and the flat metrics shape {code, message}. The original string is returned
// unchanged when it doesn't carry a parseable Azure error.
func simplifyPluginError(errMsg string) string {
	_, after, found := strings.Cut(errMsg, "body: ")
	if !found || !strings.HasPrefix(after, "{") {
		return errMsg
	}
	body := []byte(after)

	var envelope struct {
		Error *azureAPIError `json:"error"`
	}
	var e *azureAPIError
	if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
		e = envelope.Error
	} else {
		var flat azureAPIError
		if json.Unmarshal(body, &flat) != nil || flat.Message == "" {
			return errMsg
		}
		e = &flat
	}

	for e.InnerError != nil && e.InnerError.Message != "" {
		e = e.InnerError
	}
	if e.Message == "" {
		return errMsg
	}
	if e.Code != "" {
		return e.Code + ": " + e.Message
	}
	return e.Message
}

// fieldDisplayMeta returns the display name and unit for a value field,
// preferring the datasource-provided display name over the raw field name.
func fieldDisplayMeta(f dataframe.Field) (string, string) {
	name, unit := f.Name, ""
	if f.Config != nil {
		if f.Config.DisplayNameFromDS != "" {
			name = f.Config.DisplayNameFromDS
		}
		unit = f.Config.Unit
	}
	return name, unit
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

// armList is the generic Azure Resource Manager list envelope. NextLink is set
// when the result is paginated.
type armList struct {
	Value    []json.RawMessage `json:"value"`
	NextLink string            `json:"nextLink"`
}

// ParseSubscriptions parses the ARM subscriptions list response page.
func ParseSubscriptions(items []json.RawMessage) ([]Subscription, error) {
	result := make([]Subscription, 0, len(items))
	for _, raw := range items {
		var item struct {
			SubscriptionID string `json:"subscriptionId"`
			DisplayName    string `json:"displayName"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("failed to parse subscriptions: %w", err)
		}
		result = append(result, Subscription{ID: item.SubscriptionID, Name: item.DisplayName})
	}
	return result, nil
}

// ParseResourceGroups parses the ARM resource groups list response page.
func ParseResourceGroups(items []json.RawMessage) ([]ResourceGroup, error) {
	result := make([]ResourceGroup, 0, len(items))
	for _, raw := range items {
		var item struct {
			Name     string `json:"name"`
			Location string `json:"location"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("failed to parse resource groups: %w", err)
		}
		result = append(result, ResourceGroup{Name: item.Name, Location: item.Location})
	}
	return result, nil
}

// ParseResources parses the ARM resources list response page.
func ParseResources(items []json.RawMessage) ([]Resource, error) {
	result := make([]Resource, 0, len(items))
	for _, raw := range items {
		var item struct {
			Name     string `json:"name"`
			Type     string `json:"type"`
			Location string `json:"location"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("failed to parse resources: %w", err)
		}
		result = append(result, Resource{Name: item.Name, Type: item.Type, Location: item.Location})
	}
	return result, nil
}

// ParseMetricDefinitions parses the Azure Monitor metric definitions list response page.
func ParseMetricDefinitions(items []json.RawMessage) ([]MetricDefinition, error) {
	result := make([]MetricDefinition, 0, len(items))
	for _, raw := range items {
		var item struct {
			Name struct {
				Value          string `json:"value"`
				LocalizedValue string `json:"localizedValue"`
			} `json:"name"`
			PrimaryAggregationType    string   `json:"primaryAggregationType"`
			SupportedAggregationTypes []string `json:"supportedAggregationTypes"`
			Unit                      string   `json:"unit"`
			Dimensions                []struct {
				Value string `json:"value"`
			} `json:"dimensions"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("failed to parse metric definitions: %w", err)
		}

		dims := make([]string, 0, len(item.Dimensions))
		for _, d := range item.Dimensions {
			dims = append(dims, d.Value)
		}

		result = append(result, MetricDefinition{
			Name:                  item.Name.Value,
			DisplayName:           item.Name.LocalizedValue,
			PrimaryAggregation:    item.PrimaryAggregationType,
			SupportedAggregations: item.SupportedAggregationTypes,
			Unit:                  item.Unit,
			Dimensions:            dims,
		})
	}
	return result, nil
}
