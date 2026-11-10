package pyroscope

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
	"google.golang.org/protobuf/encoding/protowire"
	"k8s.io/client-go/rest"
)

const utf8LabelNamesCapability = "allow-utf8-labelnames=true"

// Client is a client for executing Pyroscope queries via Grafana's datasource API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new Pyroscope query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	httpClient.Transport = &clientCapabilitiesTransport{base: httpClient.Transport}

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// clientCapabilitiesTransport advertises Pyroscope features supported by gcx.
type clientCapabilitiesTransport struct {
	base http.RoundTripper
}

func (t *clientCapabilitiesTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}

	clone := req.Clone(req.Context())
	for _, value := range clone.Header.Values("Accept") {
		if strings.Contains(value, utf8LabelNamesCapability) {
			return base.RoundTrip(clone)
		}
	}
	clone.Header.Add("Accept", "*/*;"+utf8LabelNamesCapability)

	return base.RoundTrip(clone)
}

// Query executes a Pyroscope profile query against the specified datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	resourcePath := "querier.v1.QuerierService/SelectMergeStacktraces"
	if len(req.SpanIDs) > 0 && req.Format != ProfileFormatPprof {
		resourcePath = "querier.v1.QuerierService/SelectMergeSpanProfile"
	}
	body, contentType, err := encodeQueryRequest(req)
	if err != nil {
		return nil, err
	}
	respBody, err := c.postQueryBody(ctx, datasourceUID, resourcePath, "query", contentType, body)
	if err != nil {
		return nil, err
	}
	if req.Format == ProfileFormatPprof {
		profile, err := compressPprofResponse(respBody)
		if err != nil {
			return nil, err
		}
		return &QueryResponse{Pprof: profile}, nil
	}
	var result QueryResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &result, nil
}

// encodeQueryRequest keeps selectors identical across JSON and binary outputs.
func encodeQueryRequest(req QueryRequest) ([]byte, string, error) {
	start, end := DefaultTimeRange(req.Start, req.End)
	if req.Format == ProfileFormatPprof {
		return encodePprofRequest(req, start.UnixMilli(), end.UnixMilli()), "application/proto", nil
	}

	// Build request body
	bodyMap := map[string]any{
		"labelSelector": req.LabelSelector,
		"profileTypeID": req.ProfileTypeID,
		"start":         strconv.FormatInt(start.UnixMilli(), 10),
		"end":           strconv.FormatInt(end.UnixMilli(), 10),
	}

	if req.MaxNodes > 0 {
		bodyMap["maxNodes"] = strconv.FormatInt(req.MaxNodes, 10)
	}
	if req.Format != "" {
		bodyMap["format"] = req.Format
	}
	if len(req.SpanIDs) > 0 {
		bodyMap["spanSelector"] = req.SpanIDs
	} else {
		if len(req.ProfileIDs) > 0 {
			bodyMap["profileIdSelector"] = req.ProfileIDs
		}
		if len(req.TraceIDs) > 0 {
			bodyMap["traceIdSelector"] = req.TraceIDs
		}
		if req.StackTraceSelector != nil {
			bodyMap["stackTraceSelector"] = req.StackTraceSelector
		}
	}

	body, err := json.Marshal(bodyMap)
	return body, "application/json", err
}

// postQuery shares the bounded, authenticated RPC transport for profile queries.
func (c *Client) postQuery(ctx context.Context, datasourceUID, resourcePath, operation string, body []byte, result any) error {
	respBody, err := c.postQueryBody(ctx, datasourceUID, resourcePath, operation, "application/json", body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	return nil
}

func (c *Client) postQueryBody(ctx context.Context, datasourceUID, resourcePath, operation, contentType string, body []byte) ([]byte, error) {
	apiPath := c.buildResourcePath(datasourceUID, resourcePath)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", operation, resp.StatusCode, respBody)
	}
	return respBody, nil
}

// GetProfileStats returns ingestion stats from the datasource: whether any
// data was ever ingested and the oldest/newest profile times. The RPC is
// scoped server-side to the tenant the datasource is bound to.
func (c *Client) GetProfileStats(ctx context.Context, datasourceUID string) (*ProfileStatsResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/GetProfileStats")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBufferString("{}"))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("data range request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "data range query", resp.StatusCode, respBody)
	}

	var result ProfileStatsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// The v1 read path reports math.MaxInt64/math.MinInt64 bound sentinels
	// when no data was ever ingested (the v2 read path reports zeros).
	// Normalize any inverted range to zero so both times mean "unknown".
	if !result.DataIngested || result.OldestProfileTime > result.NewestProfileTime {
		result.OldestProfileTime = 0
		result.NewestProfileTime = 0
	}

	return &result, nil
}

// ProfileTypes returns available profile types from the datasource.
func (c *Client) ProfileTypes(ctx context.Context, datasourceUID string, req ProfileTypesRequest) (*ProfileTypesResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/ProfileTypes")

	start, end := DefaultTimeRange(req.Start, req.End)

	bodyMap := map[string]any{
		"start": strconv.FormatInt(start.UnixMilli(), 10),
		"end":   strconv.FormatInt(end.UnixMilli(), 10),
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
		return nil, fmt.Errorf("failed to get profile types: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "profile types query", resp.StatusCode, respBody)
	}

	var result ProfileTypesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// LabelNames returns label names from the datasource.
func (c *Client) LabelNames(ctx context.Context, datasourceUID string, req LabelNamesRequest) (*LabelNamesResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/LabelNames")

	start, end := DefaultTimeRange(req.Start, req.End)

	bodyMap := map[string]any{
		"start": strconv.FormatInt(start.UnixMilli(), 10),
		"end":   strconv.FormatInt(end.UnixMilli(), 10),
	}
	if len(req.Matchers) > 0 {
		bodyMap["matchers"] = req.Matchers
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
		return nil, fmt.Errorf("failed to get label names: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "label names query", resp.StatusCode, respBody)
	}

	var result LabelNamesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// LabelValues returns values for a specific label.
func (c *Client) LabelValues(ctx context.Context, datasourceUID string, req LabelValuesRequest) (*LabelValuesResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/LabelValues")

	start, end := DefaultTimeRange(req.Start, req.End)

	bodyMap := map[string]any{
		"name":  req.Name,
		"start": strconv.FormatInt(start.UnixMilli(), 10),
		"end":   strconv.FormatInt(end.UnixMilli(), 10),
	}
	if len(req.Matchers) > 0 {
		bodyMap["matchers"] = req.Matchers
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
		return nil, fmt.Errorf("failed to get label values: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "label values query", resp.StatusCode, respBody)
	}

	var result LabelValuesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// Series returns unique profile label sets from the datasource.
func (c *Client) Series(ctx context.Context, datasourceUID string, req SeriesRequest) (*SeriesResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/Series")

	start, end := DefaultTimeRange(req.Start, req.End)
	bodyMap := map[string]any{
		"start": strconv.FormatInt(start.UnixMilli(), 10),
		"end":   strconv.FormatInt(end.UnixMilli(), 10),
	}
	if len(req.Matchers) > 0 {
		bodyMap["matchers"] = req.Matchers
	}
	if len(req.LabelNames) > 0 {
		bodyMap["labelNames"] = req.LabelNames
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal series request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create series request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute series request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read series response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "profile series query", resp.StatusCode, respBody)
	}

	var result SeriesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse series response: %w", err)
	}
	if result.LabelsSet == nil {
		result.LabelsSet = []Labels{}
	}
	return &result, nil
}

func (c *Client) QueryAnomalies(ctx context.Context, datasourceUID string, req QueryAnomaliesRequest) (*QueryAnomaliesResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/QueryAnomalies")

	start, end := DefaultTimeRange(req.Start, req.End)
	anomalyTypes := req.AnomalyTypes
	if len(anomalyTypes) == 0 {
		anomalyTypes = []string{AnomalyTypeStacktrace}
	}

	bodyMap := map[string]any{
		"profileTypeID": req.ProfileTypeID,
		"labelSelector": req.LabelSelector,
		"start":         strconv.FormatInt(start.UnixMilli(), 10),
		"end":           strconv.FormatInt(end.UnixMilli(), 10),
		"anomalyTypes":  anomalyTypes,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query anomalies request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create query anomalies request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query anomalies request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read query anomalies response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "profile anomalies query", resp.StatusCode, respBody)
	}

	var result QueryAnomaliesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse query anomalies response: %w", err)
	}
	if result.StacktraceAnomalies == nil {
		result.StacktraceAnomalies = []StacktraceAnomaly{}
	}
	return &result, nil
}

// SelectSeries executes a SelectSeries query to get profile time-series data.
func (c *Client) SelectSeries(ctx context.Context, datasourceUID string, req SelectSeriesRequest) (*SelectSeriesResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/SelectSeries")

	start, end := DefaultTimeRange(req.Start, req.End)

	bodyMap := map[string]any{
		"profileTypeID": req.ProfileTypeID,
		"labelSelector": req.LabelSelector,
		"start":         strconv.FormatInt(start.UnixMilli(), 10),
		"end":           strconv.FormatInt(end.UnixMilli(), 10),
	}

	if len(req.GroupBy) > 0 {
		bodyMap["groupBy"] = req.GroupBy
	}
	if req.Step > 0 {
		bodyMap["step"] = req.Step
	}
	if req.Aggregation != "" {
		bodyMap["aggregation"] = req.Aggregation
	}
	if req.Limit > 0 {
		bodyMap["limit"] = strconv.FormatInt(req.Limit, 10)
	}
	if req.ExemplarType != "" {
		bodyMap["exemplarType"] = req.ExemplarType
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
		return nil, fmt.Errorf("failed to execute series query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "series query", resp.StatusCode, respBody)
	}

	var result SelectSeriesResponse
	dec := json.NewDecoder(bytes.NewReader(respBody))
	// UseNumber preserves numeric precision: Pyroscope's connect-rpc encodes
	// int64 timestamps as JSON strings ("1711800000000") and values as integers.
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// SelectHeatmap executes a SelectHeatmap query, used for span exemplars.
func (c *Client) SelectHeatmap(ctx context.Context, datasourceUID string, req SelectHeatmapRequest) (*SelectHeatmapResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/SelectHeatmap")

	start, end := DefaultTimeRange(req.Start, req.End)

	bodyMap := map[string]any{
		"profileTypeID": req.ProfileTypeID,
		"labelSelector": req.LabelSelector,
		"start":         strconv.FormatInt(start.UnixMilli(), 10),
		"end":           strconv.FormatInt(end.UnixMilli(), 10),
	}
	if req.Step > 0 {
		bodyMap["step"] = req.Step
	}
	if req.QueryType != "" {
		bodyMap["queryType"] = req.QueryType
	}
	if req.ExemplarType != "" {
		bodyMap["exemplarType"] = req.ExemplarType
	}
	if req.Limit > 0 {
		bodyMap["limit"] = strconv.FormatInt(req.Limit, 10)
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
		return nil, fmt.Errorf("failed to execute heatmap query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "heatmap query", resp.StatusCode, respBody)
	}

	var result SelectHeatmapResponse
	dec := json.NewDecoder(bytes.NewReader(respBody))
	dec.UseNumber()
	if err := dec.Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// Pprof fetches a merged profile via SelectMergeStacktraces and returns it as a
// gzip-compressed pprof binary, compatible with go tool pprof.
func encodePprofRequest(req QueryRequest, start, end int64) []byte {
	// Encode querier.v1.SelectMergeStacktracesRequest as binary protobuf:
	//   1: profile_type_id (string)
	//   2: label_selector (string)
	//   3: start (int64, ms since epoch)
	//   4: end (int64, ms since epoch)
	//   5: max_nodes (int64, optional)
	//   6: format (ProfileFormat, 4 = PROFILE_FORMAT_PPROF)
	//   7: stack_trace_selector (types.v1.StackTraceSelector)
	//   8: profile_id_selector (repeated string)
	//  10: trace_id_selector (repeated string)
	//  11: span_selector (repeated string)
	var msg []byte
	msg = protowire.AppendTag(msg, 1, protowire.BytesType)
	msg = protowire.AppendString(msg, req.ProfileTypeID)
	msg = protowire.AppendTag(msg, 2, protowire.BytesType)
	msg = protowire.AppendString(msg, req.LabelSelector)
	for _, field := range []struct {
		value  int64
		number protowire.Number
	}{{start, 3}, {end, 4}, {req.MaxNodes, 5}} {
		if field.number == 5 && field.value <= 0 {
			continue
		}
		msg = protowire.AppendTag(msg, field.number, protowire.VarintType)
		// Protobuf int64 uses two's-complement varints, including negative timestamps.
		msg = protowire.AppendVarint(msg, uint64(field.value)) //nolint:gosec // G115: preserve the signed int64 wire representation.
	}
	msg = protowire.AppendTag(msg, 6, protowire.VarintType)
	msg = protowire.AppendVarint(msg, 4) // PROFILE_FORMAT_PPROF
	for _, id := range req.ProfileIDs {
		msg = protowire.AppendTag(msg, 8, protowire.BytesType)
		msg = protowire.AppendString(msg, id)
	}
	for _, traceID := range req.TraceIDs {
		msg = protowire.AppendTag(msg, 10, protowire.BytesType)
		msg = protowire.AppendString(msg, traceID)
	}

	for _, spanID := range req.SpanIDs {
		msg = protowire.AppendTag(msg, 11, protowire.BytesType)
		msg = protowire.AppendString(msg, spanID)
	}
	if selector := req.StackTraceSelector; selector != nil {
		var stack []byte
		for _, location := range selector.CallSite {
			// types.v1.StackTraceSelector.call_site = 1; types.v1.Location.name = 1.
			loc := protowire.AppendTag(nil, 1, protowire.BytesType)
			loc = protowire.AppendString(loc, location.Name)
			stack = protowire.AppendTag(stack, 1, protowire.BytesType)
			stack = protowire.AppendBytes(stack, loc)
		}
		msg = protowire.AppendTag(msg, 7, protowire.BytesType)
		msg = protowire.AppendBytes(msg, stack)
	}
	return msg
}

func compressPprofResponse(body []byte) ([]byte, error) {
	var err error

	// Unwrap querier.v1.SelectMergeStacktracesResponse.pprof (field 5),
	// then querier.v1.PprofProfile.profile (field 1, google.v1.Profile).
	body, err = pprofMessageField(body, 5)
	if err != nil {
		return nil, err
	}
	body, err = pprofMessageField(body, 1)
	if err != nil {
		return nil, err
	}

	// Gzip-compress the binary proto to produce a valid pprof file.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(body); err != nil {
		return nil, fmt.Errorf("failed to compress profile: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize profile: %w", err)
	}
	return buf.Bytes(), nil
}

// pprofMessageField extracts a nested protobuf message without losing profile fields.
func pprofMessageField(data []byte, field protowire.Number) ([]byte, error) {
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, fmt.Errorf("invalid pprof response: %w", protowire.ParseError(n))
		}
		data = data[n:]
		if num == field && typ == protowire.BytesType {
			value, n := protowire.ConsumeBytes(data)
			if n < 0 {
				return nil, fmt.Errorf("invalid pprof response: %w", protowire.ParseError(n))
			}
			return value, nil
		}
		n = protowire.ConsumeFieldValue(num, typ, data)
		if n < 0 {
			return nil, fmt.Errorf("invalid pprof response: %w", protowire.ParseError(n))
		}
		data = data[n:]
	}
	if field == 5 {
		return nil, errors.New("pprof response missing PprofProfile; backend may not support PROFILE_FORMAT_PPROF")
	}
	return nil, errors.New("pprof response contains an empty PprofProfile: missing profile data")
}

func (c *Client) buildResourcePath(datasourceUID, resourcePath string) string {
	return fmt.Sprintf("/api/datasources/proxy/uid/%s/%s",
		url.PathEscape(datasourceUID), resourcePath)
}

// DefaultTimeRange returns the provided time range, or defaults to the last hour if not set.
func DefaultTimeRange(start, end time.Time) (time.Time, time.Time) {
	if start.IsZero() || end.IsZero() {
		end = time.Now()
		start = end.Add(-1 * time.Hour)
	}
	return start, end
}
