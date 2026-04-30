package pyroscope

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const maxResponseBytes = 50 << 20 // 50 MB

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
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	apiPath := c.buildResourcePath(datasourceUID, "querier.v1.QuerierService/SelectMergeStacktraces")

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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
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
