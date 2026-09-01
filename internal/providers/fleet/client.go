package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	fleetbase "github.com/grafana/gcx/internal/fleet"
)

// Fleet Management RPC paths, relative to the plugin proxy prefix.
const (
	pathListPipelines  = "/pipeline.v1.PipelineService/ListPipelines"
	pathGetPipeline    = "/pipeline.v1.PipelineService/GetPipeline"
	pathCreatePipeline = "/pipeline.v1.PipelineService/CreatePipeline"
	pathUpdatePipeline = "/pipeline.v1.PipelineService/UpdatePipeline"
	pathDeletePipeline = "/pipeline.v1.PipelineService/DeletePipeline"

	pathListCollectors  = "/collector.v1.CollectorService/ListCollectors"
	pathGetCollector    = "/collector.v1.CollectorService/GetCollector"
	pathCreateCollector = "/collector.v1.CollectorService/CreateCollector"
	pathUpdateCollector = "/collector.v1.CollectorService/UpdateCollector"
	pathDeleteCollector = "/collector.v1.CollectorService/DeleteCollector"

	pathGetLimits = "/tenant.v1.TenantService/GetLimits"
)

// Client is an HTTP client for the Grafana Fleet Management API.
// It wraps the shared base client from internal/fleet/ and adds
// pipeline-, collector-, and tenant-specific methods.
type Client struct {
	*fleetbase.Client
}

// NewClient creates a new Fleet Management client.
// baseURL must already include the collector app plugin proxy prefix.
// If httpClient is nil, a default client with a 30-second timeout is used.
func NewClient(ctx context.Context, baseURL string, httpClient *http.Client) *Client {
	return &Client{
		Client: fleetbase.NewClient(ctx, baseURL, httpClient),
	}
}

// doRequest delegates to the embedded base client's DoRequest.
func (c *Client) doRequest(ctx context.Context, path string, body any) (*http.Response, error) {
	return c.DoRequest(ctx, path, body)
}

// readErrorBody delegates to the shared error body reader.
func readErrorBody(resp *http.Response) string {
	return fleetbase.ReadErrorBody(resp)
}

// httpError builds the typed error that cmd/gcx/fail recognises. It reads the
// response body once, so callers must not read the body again.
func httpError(resp *http.Response, path string) *fleetbase.HTTPError {
	return &fleetbase.HTTPError{
		Status: resp.StatusCode,
		Path:   path,
		Body:   readErrorBody(resp),
	}
}

// pluginRouteMissing reports whether a 404 came from Grafana because the
// collector app plugin is absent or disabled, rather than from Fleet Management
// because the resource is absent.
func pluginRouteMissing(err *fleetbase.HTTPError) bool {
	return err.Status == http.StatusNotFound &&
		fleetbase.IsPluginMissingBody(err.Body)
}

// ListPipelines returns all pipelines.
func (c *Client) ListPipelines(ctx context.Context) ([]Pipeline, error) {
	resp, err := c.doRequest(ctx, pathListPipelines, map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("fleet: list pipelines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: list pipelines: %w", httpError(resp, pathListPipelines))
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
	resp, err := c.doRequest(ctx, pathGetPipeline, map[string]string{"id": id})
	if err != nil {
		return nil, fmt.Errorf("fleet: get pipeline %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		apiErr := httpError(resp, pathGetPipeline)
		if apiErr.Status == http.StatusNotFound && !pluginRouteMissing(apiErr) {
			return nil, fmt.Errorf("fleet: get pipeline %s: not found", id)
		}
		return nil, fmt.Errorf("fleet: get pipeline %s: %w", id, apiErr)
	}

	var result Pipeline
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: get pipeline %s: decode: %w", id, err)
	}

	return &result, nil
}

// CreatePipeline creates a new pipeline and returns it.
func (c *Client) CreatePipeline(ctx context.Context, p Pipeline) (*Pipeline, error) {
	resp, err := c.doRequest(ctx, pathCreatePipeline, map[string]any{"pipeline": p})
	if err != nil {
		return nil, fmt.Errorf("fleet: create pipeline: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: create pipeline: %w", httpError(resp, pathCreatePipeline))
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
	resp, err := c.doRequest(ctx, pathUpdatePipeline, map[string]any{"pipeline": p})
	if err != nil {
		return fmt.Errorf("fleet: update pipeline %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet: update pipeline %s: %w", id, httpError(resp, pathUpdatePipeline))
	}

	return nil
}

// DeletePipeline deletes a pipeline by ID.
func (c *Client) DeletePipeline(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, pathDeletePipeline, map[string]string{"id": id})
	if err != nil {
		return fmt.Errorf("fleet: delete pipeline %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet: delete pipeline %s: %w", id, httpError(resp, pathDeletePipeline))
	}

	return nil
}

// ListCollectors returns all collectors.
func (c *Client) ListCollectors(ctx context.Context) ([]Collector, error) {
	resp, err := c.doRequest(ctx, pathListCollectors, map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("fleet: list collectors: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: list collectors: %w", httpError(resp, pathListCollectors))
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
	resp, err := c.doRequest(ctx, pathGetCollector, map[string]string{"id": id})
	if err != nil {
		return nil, fmt.Errorf("fleet: get collector %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		apiErr := httpError(resp, pathGetCollector)
		if apiErr.Status == http.StatusNotFound && !pluginRouteMissing(apiErr) {
			return nil, fmt.Errorf("fleet: get collector %s: not found", id)
		}
		return nil, fmt.Errorf("fleet: get collector %s: %w", id, apiErr)
	}

	var result Collector
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: get collector %s: decode: %w", id, err)
	}

	return &result, nil
}

// CreateCollector creates a new collector and returns it.
func (c *Client) CreateCollector(ctx context.Context, col Collector) (*Collector, error) {
	resp, err := c.doRequest(ctx, pathCreateCollector, map[string]any{"collector": col})
	if err != nil {
		return nil, fmt.Errorf("fleet: create collector: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: create collector: %w", httpError(resp, pathCreateCollector))
	}

	var result Collector
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: create collector: decode: %w", err)
	}

	return &result, nil
}

// UpdateCollector updates an existing collector.
func (c *Client) UpdateCollector(ctx context.Context, col Collector) error {
	resp, err := c.doRequest(ctx, pathUpdateCollector, map[string]any{"collector": col})
	if err != nil {
		return fmt.Errorf("fleet: update collector %s: %w", col.ID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet: update collector %s: %w", col.ID, httpError(resp, pathUpdateCollector))
	}

	return nil
}

// DeleteCollector deletes a collector by ID.
func (c *Client) DeleteCollector(ctx context.Context, id string) error {
	resp, err := c.doRequest(ctx, pathDeleteCollector, map[string]string{"id": id})
	if err != nil {
		return fmt.Errorf("fleet: delete collector %s: %w", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fleet: delete collector %s: %w", id, httpError(resp, pathDeleteCollector))
	}

	return nil
}

// GetLimits returns the tenant limits for the Fleet Management stack.
func (c *Client) GetLimits(ctx context.Context) (*Limits, error) {
	resp, err := c.doRequest(ctx, pathGetLimits, map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("fleet: get limits: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: get limits: %w", httpError(resp, pathGetLimits))
	}

	var result Limits
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: get limits: decode: %w", err)
	}

	return &result, nil
}
