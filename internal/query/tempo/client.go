package tempo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

// Client is a client for executing Tempo queries via Grafana's datasource proxy API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new Tempo query client.
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

// Search searches for traces matching a TraceQL query.
func (c *Client) Search(ctx context.Context, datasourceUID string, req SearchRequest) (*SearchResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "api/search")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	if req.Query != "" {
		q.Set("q", req.Query)
	}
	if !req.Start.IsZero() {
		q.Set("start", strconv.FormatInt(req.Start.Unix(), 10))
	}
	if !req.End.IsZero() {
		q.Set("end", strconv.FormatInt(req.End.Unix(), 10))
	}
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to search traces: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("tempo", "search query", resp.StatusCode, respBody)
	}

	var result SearchResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// GetTrace retrieves a trace by its ID.
func (c *Client) GetTrace(ctx context.Context, datasourceUID string, req GetTraceRequest) (*GetTraceResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "api/v2/traces/"+url.PathEscape(req.TraceID))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	if !req.Start.IsZero() {
		q.Set("start", strconv.FormatInt(req.Start.Unix(), 10))
	}
	if !req.End.IsZero() {
		q.Set("end", strconv.FormatInt(req.End.Unix(), 10))
	}
	httpReq.URL.RawQuery = q.Encode()

	if req.LLMFormat {
		httpReq.Header.Set("Accept", AcceptLLM)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get trace: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("tempo", "get trace", resp.StatusCode, respBody)
	}

	var result GetTraceResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// Tags returns all trace tag names, optionally filtered by scope and query.
func (c *Client) Tags(ctx context.Context, datasourceUID string, req TagsRequest) (*TagsResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "api/v2/search/tags")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	if req.Scope != "" {
		q.Set("scope", req.Scope)
	}
	if req.Query != "" {
		q.Set("q", req.Query)
	}
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get tags: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("tempo", "tags query", resp.StatusCode, respBody)
	}

	var result TagsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// TagValues returns values for a specific trace tag.
func (c *Client) TagValues(ctx context.Context, datasourceUID string, req TagValuesRequest) (*TagValuesResponse, error) {
	identifier := traceQLIdentifier(req.Tag, req.Scope)
	apiPath := c.buildResourcePath(datasourceUID, "api/v2/search/tag/"+url.PathEscape(identifier)+"/values")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	if req.Query != "" {
		q.Set("q", req.Query)
	}
	httpReq.URL.RawQuery = q.Encode()

	if req.LLMFormat {
		httpReq.Header.Set("Accept", AcceptLLM)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get tag values: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("tempo", "tag values query", resp.StatusCode, respBody)
	}

	llmContentType := isLLMContentType(resp.Header.Get("Content-Type"))
	result, err := decodeTagValuesResponse(respBody, llmContentType)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	result.LLMFormat = req.LLMFormat || llmContentType

	return result, nil
}

// MetricsRange executes a TraceQL metrics range query.
func (c *Client) MetricsRange(ctx context.Context, datasourceUID string, req MetricsRequest) (*MetricsResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "api/metrics/query_range")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	q.Set("query", req.Query)
	if !req.Start.IsZero() {
		q.Set("start", strconv.FormatInt(req.Start.Unix(), 10))
	}
	if !req.End.IsZero() {
		q.Set("end", strconv.FormatInt(req.End.Unix(), 10))
	}
	if req.Step != "" {
		q.Set("step", req.Step)
	}
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("tempo", "metrics query", resp.StatusCode, respBody)
	}

	var result MetricsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	result.Instant = false

	return &result, nil
}

// MetricsInstant executes a TraceQL metrics instant query.
func (c *Client) MetricsInstant(ctx context.Context, datasourceUID string, req MetricsRequest) (*MetricsResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "api/metrics/query")

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := httpReq.URL.Query()
	q.Set("query", req.Query)
	if !req.Start.IsZero() {
		q.Set("start", strconv.FormatInt(req.Start.Unix(), 10))
	}
	if !req.End.IsZero() {
		q.Set("end", strconv.FormatInt(req.End.Unix(), 10))
	}
	httpReq.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to query metrics: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("tempo", "metrics query", resp.StatusCode, respBody)
	}

	var result MetricsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	result.Instant = true

	return &result, nil
}

// Diff compares two traces (base vs compare) via the Tempo trace-diff API.
// Delta semantics are compare - base (B - A). The endpoint is experimental and
// Grafana Cloud-only; when it is absent the datasource proxy returns a non-2xx
// status, which the caller maps to an actionable error.
func (c *Client) Diff(ctx context.Context, datasourceUID string, req DiffRequest) (DiffResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "api/v2/traces/diff")

	payload := diffPayload{
		Base:    diffTarget{TraceID: req.BaseTraceID},
		Compare: diffTarget{TraceID: req.CompareTraceID},
	}
	if !req.Start.IsZero() {
		payload.Base.Start = req.Start.Unix()
		payload.Compare.Start = req.Start.Unix()
	}
	if !req.End.IsZero() {
		payload.Base.End = req.End.Unix()
		payload.Compare.End = req.End.Unix()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode diff request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to diff traces: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// The trace-diff endpoint is experimental and Grafana Cloud-only; carry
		// those facts so the CLI can explain a route-absent failure clearly.
		return nil, queryerror.FromBody("tempo", "trace diff", resp.StatusCode, respBody).WithAvailability(true, true)
	}

	var result DiffResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result, nil
}

func (c *Client) buildResourcePath(datasourceUID, resourcePath string) string {
	return fmt.Sprintf("/api/datasources/proxy/uid/%s/%s",
		datasourceUID, resourcePath)
}

func decodeTagValuesResponse(data []byte, llmContentType bool) (*TagValuesResponse, error) {
	if llmContentType {
		return decodeLLMTagValuesResponse(data)
	}
	return decodeStandardTagValuesResponse(data)
}

func isLLMContentType(contentType string) bool {
	mediaType := strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	return mediaType == AcceptLLM || mediaType == AcceptLLM+"+json"
}

// traceQLIdentifier constructs a fully-qualified TraceQL identifier.
// If tag already contains a known scope prefix (e.g. "resource.service.name"), it is returned as-is.
// Otherwise, if scope is provided, it prepends the scope (e.g. scope="resource", tag="service.name" -> "resource.service.name").
func traceQLIdentifier(tag, scope string) string {
	if scope == "" {
		return tag
	}
	for _, s := range tagScopes {
		if strings.HasPrefix(tag, s+".") {
			return tag
		}
	}
	return scope + "." + tag
}
