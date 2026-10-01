package loki

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
)

// QueryNative executes req against Loki's own query_range/query HTTP API
// directly, via the same generic datasource resource-proxy passthrough
// Labels/Series/IndexStats already use, rather than Grafana's unified query
// API. That API's own request schema caps start/end at millisecond
// precision (a Grafana-level constraint, not a Loki one) — this path builds
// the HTTP request itself, so req.Start/req.End's full nanosecond precision
// carries straight through to Loki, uncapped.
func (c *Client) QueryNative(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	httpReq, err := c.buildNativeQueryRequest(ctx, datasourceUID, req)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := c.doNativeQuery(httpReq)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, queryerror.FromBody("loki", "query", statusCode, respBody)
	}

	var native nativeQueryResponse
	if err := json.Unmarshal(respBody, &native); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return convertNativeQueryResponse(&native), nil
}

// MetricQueryNative is QueryNative's counterpart for metric LogQL
// expressions. Loki's native vector/matrix response already matches
// MetricQueryResponse's wire shape exactly (Prometheus-style
// [seconds, value] tuples), so no conversion step is needed here.
func (c *Client) MetricQueryNative(ctx context.Context, datasourceUID string, req QueryRequest) (*MetricQueryResponse, error) {
	httpReq, err := c.buildNativeQueryRequest(ctx, datasourceUID, req)
	if err != nil {
		return nil, err
	}

	respBody, statusCode, err := c.doNativeQuery(httpReq)
	if err != nil {
		return nil, err
	}
	if statusCode != http.StatusOK {
		return nil, queryerror.FromBody("loki", "metric query", statusCode, respBody)
	}

	var result MetricQueryResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) doNativeQuery(httpReq *http.Request) ([]byte, int, error) {
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// buildNativeQueryRequest mirrors grafana-loki-datasource's own request
// construction (query/direction/limit/start/end/step or time, nanosecond
// start/end, categorize-labels encoding) so results match what the same
// query would return through Grafana's query engine, field for field.
//
// resourcePath is relative to Loki's own API root, not the full path: the
// plugin's CallResource handler itself prepends "/loki/api/v1/" to whatever
// resource path it's asked for (same convention buildLabelsPath/
// buildSeriesPath already rely on) — including that prefix here would get
// it prepended twice.
func (c *Client) buildNativeQueryRequest(ctx context.Context, datasourceUID string, req QueryRequest) (*http.Request, error) {
	qs := url.Values{}
	qs.Set("query", req.Query)
	if req.Limit > 0 {
		qs.Set("limit", strconv.Itoa(req.Limit))
	}

	var resourcePath string
	if req.IsRange() {
		qs.Set("start", strconv.FormatInt(req.Start.UnixNano(), 10))
		qs.Set("end", strconv.FormatInt(req.End.UnixNano(), 10))
		if req.Step > 0 {
			qs.Set("step", fmt.Sprintf("%dms", req.Step.Milliseconds()))
		}
		resourcePath = "query_range"
	} else {
		end := req.End
		if end.IsZero() {
			end = time.Now()
		}
		qs.Set("time", strconv.FormatInt(end.UnixNano(), 10))
		resourcePath = "query"
	}

	apiPath := fmt.Sprintf("/api/datasources/uid/%s/resources/%s?%s",
		url.PathEscape(datasourceUID), resourcePath, qs.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("X-Loki-Response-Encoding-Flags", "categorize-labels")

	return httpReq, nil
}

// nativeQueryResponse mirrors Loki's raw /loki/api/v1/query(_range) response
// shape for streams results. A thin wire-format type, converted into the
// same QueryResponse/LogEntry shape convertGrafanaResponse already produces
// by convertNativeQueryResponse, so downstream formatting code never needs
// to know which transport fetched the data.
type nativeQueryResponse struct {
	Status string             `json:"status"`
	Data   nativeQueryResults `json:"data"`
}

type nativeQueryResults struct {
	ResultType string              `json:"resultType"`
	Result     []nativeStreamEntry `json:"result"`
	Stats      *QueryStats         `json:"stats,omitempty"`
}

type nativeStreamEntry struct {
	Stream map[string]string `json:"stream"`
	Values []nativeLogEntry  `json:"values"`
}

// nativeLogEntry decodes a single [timestamp, line, metadata] tuple.
// buildNativeQueryRequest always requests the categorize-labels encoding,
// so the optional third element — when present — separates
// structuredMetadata from parsed rather than merging either into Stream.
type nativeLogEntry struct {
	Timestamp          string
	Line               string
	StructuredMetadata map[string]string
	Parsed             map[string]string
}

func (e *nativeLogEntry) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw) < 2 {
		return fmt.Errorf("loki log entry: expected at least 2 elements, got %d", len(raw))
	}
	if err := json.Unmarshal(raw[0], &e.Timestamp); err != nil {
		return fmt.Errorf("loki log entry: invalid timestamp: %w", err)
	}
	if err := json.Unmarshal(raw[1], &e.Line); err != nil {
		return fmt.Errorf("loki log entry: invalid line: %w", err)
	}
	if len(raw) < 3 {
		return nil
	}

	var metadata struct {
		StructuredMetadata map[string]string `json:"structuredMetadata"`
		Parsed             map[string]string `json:"parsed"`
	}
	if err := json.Unmarshal(raw[2], &metadata); err != nil {
		return fmt.Errorf("loki log entry: invalid metadata: %w", err)
	}
	e.StructuredMetadata = metadata.StructuredMetadata
	e.Parsed = metadata.Parsed

	return nil
}

func convertNativeQueryResponse(native *nativeQueryResponse) *QueryResponse {
	result := &QueryResponse{
		Status: native.Status,
		Data: QueryResultData{
			ResultType: native.Data.ResultType,
			Result:     make([]StreamEntry, 0, len(native.Data.Result)),
			Stats:      native.Data.Stats,
		},
	}

	for _, stream := range native.Data.Result {
		entry := StreamEntry{
			Stream: stream.Stream,
			Values: make([]LogEntry, 0, len(stream.Values)),
		}
		for _, v := range stream.Values {
			entry.Values = append(entry.Values, LogEntry(v))
		}
		result.Data.Result = append(result.Data.Result, entry)
	}

	return result
}
