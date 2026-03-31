package loki

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	apiPath := c.buildQueryPath()

	query := map[string]any{
		"refId": "A",
		"datasource": map[string]any{
			"type": "loki",
			"uid":  datasourceUID,
		},
		"expr":       req.Query,
		"intervalMs": 60000,
	}

	var from, to string
	if req.IsRange() {
		from = strconv.FormatInt(req.Start.UnixMilli(), 10)
		to = strconv.FormatInt(req.End.UnixMilli(), 10)
		if req.Step > 0 {
			query["intervalMs"] = req.Step.Milliseconds()
		}
	} else {
		from = "now-1m"
		to = "now"
		query["instant"] = true
	}

	if req.Limit > 0 {
		query["maxLines"] = req.Limit
	}

	bodyMap := map[string]any{
		"queries": []any{query},
		"from":    from,
		"to":      to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var grafanaResp GrafanaQueryResponse
	if err := json.Unmarshal(respBody, &grafanaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result, ok := grafanaResp.Results["A"]; ok {
		if result.Error != "" {
			return nil, fmt.Errorf("query error: %s", result.Error)
		}
	}

	return convertGrafanaResponse(&grafanaResp), nil
}

func (c *Client) Labels(ctx context.Context, datasourceUID string) (*LabelsResponse, error) {
	apiPath := c.buildLabelsPath(datasourceUID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("labels query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result LabelsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) LabelValues(ctx context.Context, datasourceUID, labelName string) (*LabelsResponse, error) {
	apiPath := c.buildLabelValuesPath(datasourceUID, labelName)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get label values: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("label values query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result LabelsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) Series(ctx context.Context, datasourceUID string, matchers []string) (*SeriesResponse, error) {
	apiPath := c.buildSeriesPath(datasourceUID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if len(matchers) > 0 {
		q := httpReq.URL.Query()
		for _, matcher := range matchers {
			q.Add("match[]", matcher)
		}
		httpReq.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get series: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("series query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result SeriesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) buildQueryPath() string {
	return fmt.Sprintf("/apis/query.grafana.app/v0alpha1/namespaces/%s/query",
		c.restConfig.Namespace)
}

func (c *Client) buildLabelsPath(datasourceUID string) string {
	return fmt.Sprintf("/apis/loki.datasource.grafana.app/v0alpha1/namespaces/%s/datasources/%s/resource/labels",
		c.restConfig.Namespace, datasourceUID)
}

func (c *Client) buildLabelValuesPath(datasourceUID, labelName string) string {
	return fmt.Sprintf("/apis/loki.datasource.grafana.app/v0alpha1/namespaces/%s/datasources/%s/resource/label/%s/values",
		c.restConfig.Namespace, datasourceUID, url.PathEscape(labelName))
}

func (c *Client) buildSeriesPath(datasourceUID string) string {
	return fmt.Sprintf("/apis/loki.datasource.grafana.app/v0alpha1/namespaces/%s/datasources/%s/resource/series",
		c.restConfig.Namespace, datasourceUID)
}

func convertGrafanaResponse(grafanaResp *GrafanaQueryResponse) *QueryResponse {
	result := &QueryResponse{
		Status: "success",
		Data: QueryResultData{
			ResultType: "streams",
			Result:     []StreamEntry{},
		},
	}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok {
		return result
	}

	for _, frame := range grafanaResult.Frames {
		// Extract stats and notices from frame metadata
		if frame.Schema.Meta != nil {
			frameStats := extractStats(frame.Schema.Meta.Stats)
			if frameStats != nil {
				if result.Data.Stats == nil {
					result.Data.Stats = frameStats
				} else {
					result.Data.Stats.Summary.BytesProcessedPerSecond += frameStats.Summary.BytesProcessedPerSecond
					result.Data.Stats.Summary.LinesProcessedPerSecond += frameStats.Summary.LinesProcessedPerSecond
					result.Data.Stats.Summary.TotalBytesProcessed += frameStats.Summary.TotalBytesProcessed
					result.Data.Stats.Summary.TotalLinesProcessed += frameStats.Summary.TotalLinesProcessed
					result.Data.Stats.Summary.ExecTime += frameStats.Summary.ExecTime
				}
			}
			result.Data.Notices = append(result.Data.Notices, frame.Schema.Meta.Notices...)
		}

		// Find field indices by name
		fieldIndices := make(map[string]int)
		for i, field := range frame.Schema.Fields {
			fieldIndices[field.Name] = i
		}

		labelsIdx, hasLabels := fieldIndices["labels"]
		timestampIdx, hasTimestamp := fieldIndices["timestamp"]
		bodyIdx, hasBody := fieldIndices["body"]

		// Handle log-lines format (per-line labels in values)
		if hasLabels && hasTimestamp && hasBody {
			convertLogLines(frame, labelsIdx, timestampIdx, bodyIdx, result)
			continue
		}

		// Fallback to legacy format (labels in field metadata)
		convertLegacyFormat(frame, result)
	}

	return result
}

func convertLogLines(frame DataFrame, labelsIdx, timestampIdx, bodyIdx int, result *QueryResponse) {
	if len(frame.Data.Values) <= labelsIdx ||
		len(frame.Data.Values) <= timestampIdx ||
		len(frame.Data.Values) <= bodyIdx {
		return
	}

	labelsValues := frame.Data.Values[labelsIdx]
	timestampValues := frame.Data.Values[timestampIdx]
	bodyValues := frame.Data.Values[bodyIdx]

	numEntries := len(timestampValues)
	if numEntries == 0 {
		return
	}

	// Get nanoseconds array if available
	var nanos []int
	if len(frame.Data.Nanos) > timestampIdx && frame.Data.Nanos[timestampIdx] != nil {
		nanos = frame.Data.Nanos[timestampIdx]
	}

	// Group entries by labels
	streamMap := make(map[string]*StreamEntry)
	streamOrder := make([]string, 0)

	for i := range numEntries {
		labels := parseLabels(labelsValues[i])
		ts := formatTimestampWithNanos(timestampValues[i], nanos, i)
		body := toString(bodyValues[i])

		key := labelsKey(labels)
		entry, exists := streamMap[key]
		if !exists {
			entry = &StreamEntry{
				Stream: labels,
				Values: make([][]string, 0),
			}
			streamMap[key] = entry
			streamOrder = append(streamOrder, key)
		}
		entry.Values = append(entry.Values, []string{ts, body})
	}

	for _, key := range streamOrder {
		result.Data.Result = append(result.Data.Result, *streamMap[key])
	}
}

func convertLegacyFormat(frame DataFrame, result *QueryResponse) {
	if len(frame.Schema.Fields) < 2 || len(frame.Data.Values) < 2 {
		return
	}

	var timeIdx, valueIdx = -1, -1
	var labels map[string]string

	for i, field := range frame.Schema.Fields {
		switch field.Type {
		case "time":
			timeIdx = i
		case "string", "number":
			valueIdx = i
		}
		if len(field.Labels) > 0 {
			labels = field.Labels
		}
	}

	if timeIdx == -1 || valueIdx == -1 {
		return
	}

	timeValues := frame.Data.Values[timeIdx]
	dataValues := frame.Data.Values[valueIdx]

	if len(timeValues) == 0 || len(dataValues) == 0 {
		return
	}

	entry := StreamEntry{
		Stream: labels,
		Values: make([][]string, 0, len(timeValues)),
	}

	for i := range timeValues {
		ts := formatTimestamp(timeValues[i])
		value := toString(dataValues[i])
		entry.Values = append(entry.Values, []string{ts, value})
	}

	result.Data.Result = append(result.Data.Result, entry)
}

func extractStats(frameStats []FrameStat) *QueryStats {
	if len(frameStats) == 0 {
		return nil
	}

	stats := &QueryStats{}
	for _, s := range frameStats {
		switch s.DisplayName {
		case "Summary: bytes processed per second":
			stats.Summary.BytesProcessedPerSecond = int64(s.Value)
		case "Summary: lines processed per second":
			stats.Summary.LinesProcessedPerSecond = int64(s.Value)
		case "Summary: total bytes processed":
			stats.Summary.TotalBytesProcessed = int64(s.Value)
		case "Summary: total lines processed":
			stats.Summary.TotalLinesProcessed = int64(s.Value)
		case "Summary: exec time":
			stats.Summary.ExecTime = s.Value
		}
	}
	return stats
}

func parseLabels(v any) map[string]string {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		labels := make(map[string]string, len(m))
		for k, val := range m {
			labels[k] = toString(val)
		}
		return labels
	}
	if m, ok := v.(map[string]string); ok {
		return m
	}
	return nil
}

func labelsKey(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return ""
	}
	return string(b)
}

func formatTimestampWithNanos(v any, nanos []int, idx int) string {
	var millis int64
	switch val := v.(type) {
	case float64:
		millis = int64(val)
	case int64:
		millis = val
	case int:
		millis = int64(val)
	default:
		return fmt.Sprintf("%v", v)
	}

	// Convert to nanoseconds: millis * 1e6 + nanos
	nanosTotal := millis * 1e6
	if nanos != nil && idx < len(nanos) {
		nanosTotal += int64(nanos[idx])
	}
	return strconv.FormatInt(nanosTotal, 10)
}

func formatTimestamp(v any) string {
	switch val := v.(type) {
	case float64:
		nanos := int64(val * 1e6)
		return strconv.FormatInt(nanos, 10)
	case int64:
		return strconv.FormatInt(val*1e6, 10)
	case string:
		return val
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}
