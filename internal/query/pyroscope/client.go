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
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
	"google.golang.org/protobuf/encoding/protowire"
	"k8s.io/client-go/rest"
)

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

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// Query executes a Pyroscope profile query against the specified datasource.
// Span-scoped queries deliberately use the deprecated SelectMergeSpanProfile
// RPC: on SelectMergeStacktraces, backends that predate the span_selector
// field silently discard it and return an unfiltered flamegraph — there is no
// way to detect that from the response. (profilecli detects old servers via
// the pprof payload of format=PPROF and converts it, but gcx would then have
// to rebuild a flamegraph from raw pprof client-side, which is not worth it.)
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	resourcePath := "querier.v1.QuerierService/SelectMergeStacktraces"
	if len(req.SpanIDs) > 0 {
		resourcePath = "querier.v1.QuerierService/SelectMergeSpanProfile"
	}
	apiPath := c.buildResourcePath(datasourceUID, resourcePath)

	start, end := DefaultTimeRange(req.Start, req.End)

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

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "query", resp.StatusCode, respBody)
	}

	var result QueryResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// isAPIError reports whether err is a server response error (as opposed to a
// transport failure or cancellation), i.e. a fallback request could succeed.
func isAPIError(err error) bool {
	var apiErr *queryerror.APIError
	return errors.As(err, &apiErr)
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

// profileFormatPprof is querier.v1.ProfileFormat_PROFILE_FORMAT_PPROF.
const profileFormatPprof = 4

// Pprof fetches a merged profile and returns it as a gzip-compressed pprof
// binary, compatible with go tool pprof. It requests SelectMergeStacktraces
// with PROFILE_FORMAT_PPROF first; backends that predate the format field
// ignore it and return a flamegraph payload instead, in which case (or when
// the server rejects the request) it falls back to the deprecated
// SelectMergeProfile RPC — the same strategy profilecli uses.
func (c *Client) Pprof(ctx context.Context, datasourceUID string, req PprofRequest) ([]byte, error) {
	start, end := DefaultTimeRange(req.Start, req.End)

	respBody, err := c.postProto(ctx, datasourceUID, "querier.v1.QuerierService/SelectMergeStacktraces",
		encodePprofStacktracesRequest(req, start, end))
	if err == nil {
		if profile, ok := extractStacktracesPprof(respBody); ok {
			return gzipProfile(profile)
		}
	} else if !isAPIError(err) {
		return nil, err
	}

	respBody, err = c.postProto(ctx, datasourceUID, "querier.v1.QuerierService/SelectMergeProfile",
		encodeSelectMergeProfileRequest(req, start, end))
	if err != nil {
		return nil, err
	}
	return gzipProfile(respBody)
}

// encodePprofStacktracesRequest encodes a SelectMergeStacktracesRequest as
// binary protobuf, requesting pprof output. Field numbers from querier.v1:
//
//	1: profile_typeID  2: label_selector  3: start  4: end  5: max_nodes
//	6: format  7: stack_trace_selector  8: profile_id_selector
//	10: trace_id_selector
func encodePprofStacktracesRequest(req PprofRequest, start, end time.Time) []byte {
	msg := encodeCommonProfileFields(req, start, end)
	msg = protowire.AppendTag(msg, 6, protowire.VarintType)
	msg = protowire.AppendVarint(msg, profileFormatPprof)
	msg = appendStackTraceSelector(msg, 7, req.StackTraceSelector)
	msg = appendStrings(msg, 8, req.ProfileIDs)
	msg = appendStrings(msg, 10, req.TraceIDs)
	return msg
}

// encodeSelectMergeProfileRequest encodes a SelectMergeProfileRequest as
// binary protobuf. Field numbers from querier.v1:
//
//	1: profile_typeID  2: label_selector  3: start  4: end  5: max_nodes
//	6: stack_trace_selector  7: profile_id_selector  8: trace_id_selector
func encodeSelectMergeProfileRequest(req PprofRequest, start, end time.Time) []byte {
	msg := encodeCommonProfileFields(req, start, end)
	msg = appendStackTraceSelector(msg, 6, req.StackTraceSelector)
	msg = appendStrings(msg, 7, req.ProfileIDs)
	msg = appendStrings(msg, 8, req.TraceIDs)
	return msg
}

// encodeCommonProfileFields encodes the fields shared by both merge-profile
// request messages (identical numbers 1-5 in querier.v1).
func encodeCommonProfileFields(req PprofRequest, start, end time.Time) []byte {
	var msg []byte
	msg = protowire.AppendTag(msg, 1, protowire.BytesType)
	msg = protowire.AppendString(msg, req.ProfileTypeID)
	msg = protowire.AppendTag(msg, 2, protowire.BytesType)
	msg = protowire.AppendString(msg, req.LabelSelector)
	msg = protowire.AppendTag(msg, 3, protowire.VarintType)
	msg = protowire.AppendVarint(msg, uint64(start.UnixMilli()))
	msg = protowire.AppendTag(msg, 4, protowire.VarintType)
	msg = protowire.AppendVarint(msg, uint64(end.UnixMilli()))
	if req.MaxNodes > 0 {
		msg = protowire.AppendTag(msg, 5, protowire.VarintType)
		msg = protowire.AppendVarint(msg, uint64(req.MaxNodes))
	}
	return msg
}

func appendStrings(msg []byte, field protowire.Number, values []string) []byte {
	for _, v := range values {
		msg = protowire.AppendTag(msg, field, protowire.BytesType)
		msg = protowire.AppendString(msg, v)
	}
	return msg
}

// appendStackTraceSelector encodes a types.v1.StackTraceSelector
// (call_site = repeated Location{name = 1}) into the given field.
func appendStackTraceSelector(msg []byte, field protowire.Number, sel *StackTraceSelector) []byte {
	if sel == nil || len(sel.CallSite) == 0 {
		return msg
	}
	var sub []byte
	for _, loc := range sel.CallSite {
		var l []byte
		l = protowire.AppendTag(l, 1, protowire.BytesType)
		l = protowire.AppendString(l, loc.Name)
		sub = protowire.AppendTag(sub, 1, protowire.BytesType)
		sub = protowire.AppendBytes(sub, l)
	}
	msg = protowire.AppendTag(msg, field, protowire.BytesType)
	msg = protowire.AppendBytes(msg, sub)
	return msg
}

// extractStacktracesPprof pulls the raw google.v1.Profile bytes out of a
// binary SelectMergeStacktracesResponse (field 5 = PprofProfile{1: profile}).
// ok is false when the payload is absent or malformed — meaning the backend
// ignored the format field and answered with a flamegraph.
func extractStacktracesPprof(body []byte) ([]byte, bool) {
	var profile []byte
	for len(body) > 0 {
		num, typ, n := protowire.ConsumeTag(body)
		if n < 0 {
			return nil, false
		}
		body = body[n:]
		if num != 5 || typ != protowire.BytesType {
			if n = protowire.ConsumeFieldValue(num, typ, body); n < 0 {
				return nil, false
			}
			body = body[n:]
			continue
		}
		wrapper, n := protowire.ConsumeBytes(body)
		if n < 0 {
			return nil, false
		}
		body = body[n:]
		for len(wrapper) > 0 {
			wnum, wtyp, wn := protowire.ConsumeTag(wrapper)
			if wn < 0 {
				return nil, false
			}
			wrapper = wrapper[wn:]
			if wnum == 1 && wtyp == protowire.BytesType {
				p, pn := protowire.ConsumeBytes(wrapper)
				if pn < 0 {
					return nil, false
				}
				wrapper = wrapper[pn:]
				profile = p
				continue
			}
			if wn = protowire.ConsumeFieldValue(wnum, wtyp, wrapper); wn < 0 {
				return nil, false
			}
			wrapper = wrapper[wn:]
		}
	}
	return profile, len(profile) > 0
}

// postProto posts a binary protobuf request to the given querier RPC and
// returns the raw response body.
func (c *Client) postProto(ctx context.Context, datasourceUID, rpc string, msg []byte) ([]byte, error) {
	apiPath := c.buildResourcePath(datasourceUID, rpc)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewReader(msg))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/proto")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch profile: %w", err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("pyroscope", "pprof", resp.StatusCode, body)
	}
	return body, nil
}

// gzipProfile compresses raw profile proto bytes into a valid pprof file.
func gzipProfile(profile []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(profile); err != nil {
		return nil, fmt.Errorf("failed to compress profile: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize profile: %w", err)
	}
	return buf.Bytes(), nil
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
