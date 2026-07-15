package cloudmonitoring

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/queryerror"
)

// QueryRequest represents a Google Cloud Monitoring time-series list query.
//
// Filters maps label keys to values; each entry becomes a (key, =, value)
// triplet alongside the metric type filter. GroupBys splits the result into
// one series per combination of the given labels.
type QueryRequest struct {
	Project         string
	MetricType      string
	Reducer         string
	Aligner         string
	AlignmentPeriod string
	GroupBys        []string
	Filters         map[string]string
	Start           time.Time
	End             time.Time
}

// Frame represents a single time-series frame from a query result.
type Frame struct {
	Name       string            `json:"name"`
	Labels     map[string]string `json:"labels,omitempty"`
	Timestamps []time.Time       `json:"timestamps"`
	Values     []*float64        `json:"values"`
}

// QueryResponse holds the parsed query result.
type QueryResponse struct {
	Frames []Frame `json:"frames"`
}

// Project represents a GCP project visible to the datasource.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// MetricDescriptor represents a Cloud Monitoring metric descriptor.
type MetricDescriptor struct {
	Type        string `json:"type"`
	DisplayName string `json:"displayName"`
	MetricKind  string `json:"metricKind"`
	ValueType   string `json:"valueType"`
	Unit        string `json:"unit,omitempty"`
	Service     string `json:"service,omitempty"`
}

// ParseQueryResponse converts the raw Grafana response bytes into a QueryResponse.
func ParseQueryResponse(body []byte) (*QueryResponse, error) {
	var raw dataframe.Response
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse cloudmonitoring response: %w", err)
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
		return nil, queryerror.New("cloudmonitoring", "query", status, simplifyGCPError(result.Error), result.ErrorSource)
	}

	resp := &QueryResponse{Frames: make([]Frame, 0, len(result.Frames))}
	for _, df := range result.Frames {
		if frame, ok := parseFrame(df); ok {
			resp.Frames = append(resp.Frames, frame)
		}
	}
	return resp, nil
}

// parseFrame extracts the first time/value column pair with its labels and
// datasource-provided display name.
func parseFrame(df dataframe.Frame) (Frame, bool) {
	if len(df.Schema.Fields) != len(df.Data.Values) || len(df.Data.Values) < 2 {
		return Frame{}, false
	}

	timeIdx, valueIdx := -1, -1
	frame := Frame{}
	for i, f := range df.Schema.Fields {
		switch {
		case f.Type == "time" && timeIdx == -1:
			timeIdx = i
		case f.Type == "number" && valueIdx == -1:
			valueIdx = i
			frame.Labels = f.Labels
			frame.Name = f.Name
			if f.Config != nil && f.Config.DisplayNameFromDS != "" {
				frame.Name = f.Config.DisplayNameFromDS
			}
		}
		if timeIdx != -1 && valueIdx != -1 {
			break
		}
	}
	if timeIdx == -1 || valueIdx == -1 {
		return Frame{}, false
	}

	times, values := df.Data.Values[timeIdx], df.Data.Values[valueIdx]
	n := min(len(times), len(values))
	for i := range n {
		ms, ok := toFloat64(times[i])
		if !ok {
			continue
		}
		frame.Timestamps = append(frame.Timestamps, time.UnixMilli(int64(ms)).UTC())
		if values[i] == nil {
			frame.Values = append(frame.Values, nil)
			continue
		}
		if v, ok := toFloat64(values[i]); ok {
			frame.Values = append(frame.Values, &v)
		} else {
			frame.Timestamps = frame.Timestamps[:len(frame.Timestamps)-1]
		}
	}

	return frame, len(frame.Timestamps) > 0
}

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
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

// simplifyGCPError reduces a GCP error envelope embedded in the plugin's
// error string (e.g. `query failed: {"error": {"code", "message", "status"}}`)
// to its status and message. The original string is returned unchanged when no
// parseable envelope is found.
func simplifyGCPError(errMsg string) string {
	idx := strings.Index(errMsg, "{")
	if idx == -1 {
		return errMsg
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(errMsg[idx:]), &envelope) != nil || envelope.Error.Message == "" {
		return errMsg
	}
	if envelope.Error.Status != "" {
		return envelope.Error.Status + ": " + envelope.Error.Message
	}
	return envelope.Error.Message
}

// ParseProjects parses the /resources/projects response
// (shape: [{"label": "...", "value": "..."}]).
func ParseProjects(body []byte) ([]Project, error) {
	var items []struct {
		Label string `json:"label"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse projects: %w", err)
	}
	projects := make([]Project, 0, len(items))
	for _, item := range items {
		projects = append(projects, Project{ID: item.Value, Name: item.Label})
	}
	return projects, nil
}

// ParseMetricDescriptors parses the metricDescriptors resource response.
func ParseMetricDescriptors(body []byte) ([]MetricDescriptor, error) {
	var items []MetricDescriptor
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("failed to parse metric descriptors: %w", err)
	}
	return items, nil
}
