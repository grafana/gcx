package tempo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trace search failed with status %d: %s", resp.StatusCode, string(respBody))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get trace failed with status %d: %s", resp.StatusCode, string(respBody))
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tags query failed with status %d: %s", resp.StatusCode, string(respBody))
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

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get tag values: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tag values query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result TagValuesResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result MetricsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result MetricsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) buildResourcePath(datasourceUID, resourcePath string) string {
	return fmt.Sprintf("/api/datasources/proxy/uid/%s/%s",
		datasourceUID, resourcePath)
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
