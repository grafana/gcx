package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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

	// pathCallCollector is on a different backend (the tunnel service, not
	// fleet-management-api), but reaches it through the same plugin proxy
	// under a distinct path prefix declared in grafana-collector-app's
	// plugin.json (tunnelClusterUrl, not agmClusterUrl).
	pathCallCollector = "/tunnel-api/tunnel.v1.TunnelService/CallCollector"
)

// TunnelHTTPRequest mirrors tunnel.v1.HTTPRequest. Method uses the proto enum's
// string name (e.g. "HTTP_REQUEST_METHOD_GET"); see httpMethodToProto.
type TunnelHTTPRequest struct {
	Method      string `json:"method"`
	RequestURI  string `json:"requestUri"`
	ContentType string `json:"contentType,omitempty"`
	Body        []byte `json:"body,omitempty"` // encoding/json base64-encodes []byte, matching protojson's bytes representation.
}

// TunnelHTTPResponse mirrors tunnel.v1.HTTPResponse.
type TunnelHTTPResponse struct {
	Status     int    `json:"status"`
	RequestURI string `json:"requestUri"`
	Body       []byte `json:"body,omitempty"`
}

// httpMethodToProto maps a plain HTTP method string to tunnel.v1.HTTPRequestMethod's
// proto enum name. Empty or unrecognized input defaults to GET.
func httpMethodToProto(method string) string {
	switch strings.ToUpper(method) {
	case "POST":
		return "HTTP_REQUEST_METHOD_POST"
	case "PUT":
		return "HTTP_REQUEST_METHOD_PUT"
	case "DELETE":
		return "HTTP_REQUEST_METHOD_DELETE"
	case "PATCH":
		return "HTTP_REQUEST_METHOD_PATCH"
	default:
		return "HTTP_REQUEST_METHOD_GET"
	}
}

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

	// The production API can return an empty or partial object after a
	// successful create. Start with the submitted collector so callers retain
	// the canonical ID and name when the response omits them.
	result := col
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

// CallCollector sends an HTTP request to a collector through its open Fleet
// Management tunnel (tunnel.v1.TunnelService/CallCollector) and returns the
// collector's response. Returns a not-found-shaped error if the collector
// has no open tunnel.
func (c *Client) CallCollector(ctx context.Context, collectorID string, req TunnelHTTPRequest) (*TunnelHTTPResponse, error) {
	resp, err := c.doRequest(ctx, pathCallCollector, map[string]any{
		"collectorId": collectorID,
		"request":     req,
	})
	if err != nil {
		return nil, fmt.Errorf("fleet: call collector %s: %w", collectorID, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fleet: call collector %s: %w", collectorID, httpError(resp, pathCallCollector))
	}

	var result struct {
		CollectorID string             `json:"collectorId"`
		Response    TunnelHTTPResponse `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("fleet: call collector %s: decode: %w", collectorID, err)
	}

	return &result.Response, nil
}
