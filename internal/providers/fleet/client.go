package fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is an HTTP client for the Grafana Fleet Management API.
// All operations use POST (gRPC/Connect style).
type Client struct {
	baseURL      string
	instanceID   string
	apiToken     string
	useBasicAuth bool
	httpClient   *http.Client
}

// NewClient creates a new Fleet Management client.
// When useBasicAuth is true, requests use Basic auth with instanceID:apiToken.
// Otherwise, requests use Bearer token auth.
func NewClient(baseURL, instanceID, apiToken string, useBasicAuth bool) *Client {
	return &Client{
		baseURL:      strings.TrimRight(baseURL, "/"),
		instanceID:   instanceID,
		apiToken:     apiToken,
		useBasicAuth: useBasicAuth,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
	}
}

// doRequest builds and executes a POST request against the Fleet Management API.
func (c *Client) doRequest(ctx context.Context, path string, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("fleet: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("fleet: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if c.useBasicAuth {
		req.SetBasicAuth(c.instanceID, c.apiToken)
	} else {
		req.Header.Set("Authorization", "Bearer "+c.apiToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fleet: execute request: %w", err)
	}

	return resp, nil
}

// readErrorBody reads and returns the response body as a string for error messages.
func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "(could not read body)"
	}
	return string(body)
}

// ListPipelines returns all pipelines.
func (c *Client) ListPipelines(ctx context.Context) ([]Pipeline, error) {
	resp, err := c.doRequest(ctx, "/pipeline.v1.PipelineService/ListPipelines", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("fleet: list pipelines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: list pipelines: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	var result struct {
		Pipelines []Pipeline `json:"pipelines"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: list pipelines: decode: %w", err)
	}

	return result.Pipelines, nil
}

// GetPipeline returns a single pipeline by ID. Returns nil if not found.
func (c *Client) GetPipeline(ctx context.Context, id string) (*Pipeline, error) {
	resp, err := c.doRequest(ctx, "/pipeline.v1.PipelineService/GetPipeline", map[string]string{"id": id})
	if err != nil {
		return nil, fmt.Errorf("fleet: get pipeline %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("fleet: get pipeline %s: not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: get pipeline %s: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	var result Pipeline
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: get pipeline %s: decode: %w", id, err)
	}

	return &result, nil
}

// CreatePipeline creates a new pipeline and returns it.
func (c *Client) CreatePipeline(ctx context.Context, p Pipeline) (*Pipeline, error) {
	resp, err := c.doRequest(ctx, "/pipeline.v1.PipelineService/CreatePipeline", map[string]any{"pipeline": p})
	if err != nil {
		return nil, fmt.Errorf("fleet: create pipeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: create pipeline: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	var result Pipeline
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: create pipeline: decode: %w", err)
	}

	return &result, nil
}

// UpdatePipeline updates an existing pipeline.
func (c *Client) UpdatePipeline(ctx context.Context, id string, p Pipeline) error {
	p.ID = id
	resp, err := c.doRequest(ctx, "/pipeline.v1.PipelineService/UpdatePipeline", map[string]any{"pipeline": p})
	if err != nil {
		return fmt.Errorf("fleet: update pipeline %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet: update pipeline %s: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	return nil
}

// DeletePipeline deletes a pipeline by ID.
func (c *Client) DeletePipeline(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, "/pipeline.v1.PipelineService/DeletePipeline", map[string]string{"id": id})
	if err != nil {
		return fmt.Errorf("fleet: delete pipeline %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet: delete pipeline %s: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	return nil
}

// ListCollectors returns all collectors.
func (c *Client) ListCollectors(ctx context.Context) ([]Collector, error) {
	resp, err := c.doRequest(ctx, "/collector.v1.CollectorService/ListCollectors", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("fleet: list collectors: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: list collectors: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	var result struct {
		Collectors []Collector `json:"collectors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: list collectors: decode: %w", err)
	}

	return result.Collectors, nil
}

// GetCollector returns a single collector by ID. Returns nil if not found.
func (c *Client) GetCollector(ctx context.Context, id string) (*Collector, error) {
	resp, err := c.doRequest(ctx, "/collector.v1.CollectorService/GetCollector", map[string]string{"id": id})
	if err != nil {
		return nil, fmt.Errorf("fleet: get collector %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("fleet: get collector %s: not found", id)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: get collector %s: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	var result Collector
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: get collector %s: decode: %w", id, err)
	}

	return &result, nil
}

// CreateCollector creates a new collector and returns it.
func (c *Client) CreateCollector(ctx context.Context, col Collector) (*Collector, error) {
	resp, err := c.doRequest(ctx, "/collector.v1.CollectorService/CreateCollector", map[string]any{"collector": col})
	if err != nil {
		return nil, fmt.Errorf("fleet: create collector: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: create collector: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	var result Collector
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: create collector: decode: %w", err)
	}

	return &result, nil
}

// UpdateCollector updates an existing collector.
func (c *Client) UpdateCollector(ctx context.Context, col Collector) error {
	resp, err := c.doRequest(ctx, "/collector.v1.CollectorService/UpdateCollector", map[string]any{"collector": col})
	if err != nil {
		return fmt.Errorf("fleet: update collector %s: %w", col.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet: update collector %s: status %d: %s", col.ID, resp.StatusCode, readErrorBody(resp))
	}

	return nil
}

// DeleteCollector deletes a collector by ID.
func (c *Client) DeleteCollector(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, "/collector.v1.CollectorService/DeleteCollector", map[string]string{"id": id})
	if err != nil {
		return fmt.Errorf("fleet: delete collector %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet: delete collector %s: status %d: %s", id, resp.StatusCode, readErrorBody(resp))
	}

	return nil
}

// GetLimits returns the tenant limits for the Fleet Management stack.
func (c *Client) GetLimits(ctx context.Context) (*Limits, error) {
	resp, err := c.doRequest(ctx, "/tenant.v1.TenantService/GetLimits", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("fleet: get limits: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: get limits: status %d: %s", resp.StatusCode, readErrorBody(resp))
	}

	var result Limits
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: get limits: decode: %w", err)
	}

	return &result, nil
}
