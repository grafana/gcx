package kg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafanactl/internal/config"
	"k8s.io/client-go/rest"
)

const pluginResourcePath = "/api/plugins/grafana-asserts-app/resources"

const (
	statusPath       = pluginResourcePath + "/asserts/api-server/v1/stack/status"
	entitiesPath     = pluginResourcePath + "/asserts/api-server/v1/entity/info"
	entityTypesPath  = pluginResourcePath + "/asserts/api-server/v1/entity_type"
	scopesPath       = pluginResourcePath + "/asserts/api-server/v1/entity_scope"
	assertionsPath   = pluginResourcePath + "/asserts/api-server/v1/assertions"
	searchPath       = pluginResourcePath + "/asserts/api-server/v1/search"
	rulesPath        = pluginResourcePath + "/asserts/api-server/v1/config/prom-rules/"
	environmentPath  = pluginResourcePath + "/asserts/api-server/v1/config/environment"
	entityLookupPath = pluginResourcePath + "/asserts/api-server/v1/entity"
	graphDisplayPath = pluginResourcePath + "/asserts/api-server/v1/config/display/graph"
)

// Client is an HTTP client for the Knowledge Graph (Asserts) API.
type Client struct {
	httpClient *http.Client
	host       string
}

// NewClient creates a new KG client from the given REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("kg: failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host}, nil
}

// getJSON performs a GET request and decodes the JSON response into v.
func (c *Client) getJSON(ctx context.Context, path string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return fmt.Errorf("kg: create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return readError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// postJSON performs a POST request with a JSON body and decodes the response into v.
// If v is nil, the response body is discarded.
func (c *Client) postJSON(ctx context.Context, path string, body, v any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, v)
}

// putJSON performs a PUT request with a JSON body and decodes the response into v.
func (c *Client) putJSON(ctx context.Context, path string, body, v any) error {
	return c.doJSON(ctx, http.MethodPut, path, body, v)
}

// doJSON performs an HTTP request with a JSON body and decodes the response into v.
func (c *Client) doJSON(ctx context.Context, method, path string, body, v any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("kg: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.host+path, bodyReader)
	if err != nil {
		return fmt.Errorf("kg: create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return readError(resp)
	}
	if v != nil {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}

// doYAML performs an HTTP request with a YAML body.
func (c *Client) doYAML(ctx context.Context, method, path, yamlContent string) error {
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, strings.NewReader(yamlContent))
	if err != nil {
		return fmt.Errorf("kg: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-yaml")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return readError(resp)
	}
	return nil
}

// APIError is a structured error returned by the KG API.
type APIError struct {
	StatusCode int
	message    string // extracted from JSON body, if available
	rawBody    string
}

func (e *APIError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("kg: request failed with status %d: %s", e.StatusCode, e.message)
	}
	if e.rawBody != "" {
		return fmt.Sprintf("kg: request failed with status %d: %s", e.StatusCode, e.rawBody)
	}
	return fmt.Sprintf("kg: request failed with status %d", e.StatusCode)
}

// IsServerError returns true for 5xx status codes.
func (e *APIError) IsServerError() bool {
	return e.StatusCode >= 500
}

// readError reads the response body and returns a formatted APIError.
func readError(resp *http.Response) *APIError {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &APIError{StatusCode: resp.StatusCode}
	}
	apiErr := &APIError{StatusCode: resp.StatusCode}
	if len(body) > 0 {
		// Try to extract a human-readable message from a JSON error body.
		var jsonErr struct {
			Message string `json:"message"`
		}
		if jsonErr2 := json.Unmarshal(body, &jsonErr); jsonErr2 == nil && jsonErr.Message != "" {
			apiErr.message = jsonErr.Message
		} else {
			apiErr.rawBody = string(body)
		}
	}
	return apiErr
}

// ---------------------------------------------------------------------------
// Lifecycle operations
// ---------------------------------------------------------------------------

// Setup initializes the Asserts plugin.
func (c *Client) Setup(ctx context.Context) error {
	return c.postJSON(ctx, pluginResourcePath+"/asserts-setup", struct{}{}, nil)
}

// Enable enables the Knowledge Graph feature.
func (c *Client) Enable(ctx context.Context) error {
	return c.postJSON(ctx, pluginResourcePath+"/asserts/api-server/v2/stack/enable", struct{}{}, nil)
}

// GetStatus retrieves the current Knowledge Graph status.
func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	var status Status
	if err := c.getJSON(ctx, statusPath, &status); err != nil {
		return nil, fmt.Errorf("kg: get status: %w", err)
	}
	return &status, nil
}

// ---------------------------------------------------------------------------
// Dataset operations
// ---------------------------------------------------------------------------

// GetDatasets retrieves the current dataset configuration.
func (c *Client) GetDatasets(ctx context.Context) (*DatasetsResponse, error) {
	var result DatasetsResponse
	if err := c.getJSON(ctx, pluginResourcePath+"/asserts/api-server/v2/stack/datasets", &result); err != nil {
		return nil, fmt.Errorf("kg: get datasets: %w", err)
	}
	return &result, nil
}

// ActivateDataset activates a dataset (kubernetes, otel, prometheus, aws, etc).
func (c *Client) ActivateDataset(ctx context.Context, dataset string, cfg DatasetConfig) error {
	body := DatasetActivationRequest{
		DatasetType:     dataset,
		DisabledVendors: []string{},
		FilterGroups:    cfg.FilterGroups,
	}
	if len(body.FilterGroups) == 0 {
		body.FilterGroups = []FilterGroup{{
			Filters:         []string{},
			EnvLabel:        defaultEnvLabel(dataset),
			SiteLabel:       "",
			EnvLabelValues:  []string{},
			SiteLabelValues: []string{},
		}}
	}
	return c.putJSON(ctx, pluginResourcePath+"/asserts/api-server/v2/stack/dataset", body, nil)
}

// GetVendors retrieves the list of detected vendors.
// The API may return either {"vendors": [...]} or a bare [...] array.
func (c *Client) GetVendors(ctx context.Context) ([]Vendor, error) {
	path := pluginResourcePath + "/asserts/api-server/v2/stack/dataset/prometheus/vendors"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("kg: create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, readError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kg: read vendors response: %w", err)
	}

	// The API returns {"items": [...]} with vendor objects.
	var wrapped struct {
		Items []Vendor `json:"items"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		return nil, fmt.Errorf("kg: decode vendors: %w", err)
	}
	return wrapped.Items, nil
}

func defaultEnvLabel(dataset string) string {
	switch dataset {
	case "otel":
		return "k8s_cluster_name"
	case "aws":
		return "account_id"
	case "azure":
		return "subscription_id"
	case "gcp":
		return "project_id"
	default:
		return "cluster"
	}
}

// ---------------------------------------------------------------------------
// Configuration upload operations
// ---------------------------------------------------------------------------

// UploadPromRules uploads Prometheus recording rules.
func (c *Client) UploadPromRules(ctx context.Context, yamlContent string) error {
	return c.doYAML(ctx, http.MethodPut, rulesPath, yamlContent)
}

// UploadModelRules uploads model rules configuration.
func (c *Client) UploadModelRules(ctx context.Context, yamlContent string) error {
	return c.doYAML(ctx, http.MethodPut, pluginResourcePath+"/asserts/api-server/v1/config/model-rules/", yamlContent)
}

// UploadSuppressions uploads alert suppression configuration.
func (c *Client) UploadSuppressions(ctx context.Context, yamlContent string) error {
	return c.doYAML(ctx, http.MethodPost, pluginResourcePath+"/asserts/api-server/v1/config/disabled-alerts/", yamlContent)
}

// UploadRelabelRules uploads relabel rules configuration.
func (c *Client) UploadRelabelRules(ctx context.Context, yamlContent string) error {
	return c.doYAML(ctx, http.MethodPut, pluginResourcePath+"/asserts/api-server/v2/config/relabel-rules/prologue", yamlContent)
}

// ---------------------------------------------------------------------------
// Environment & dashboard configuration
// ---------------------------------------------------------------------------

// GetEnvironment retrieves the current environment configuration.
func (c *Client) GetEnvironment(ctx context.Context) (*EnvironmentConfig, error) {
	var cfg EnvironmentConfig
	if err := c.getJSON(ctx, environmentPath, &cfg); err != nil {
		return nil, fmt.Errorf("kg: get environment: %w", err)
	}
	return &cfg, nil
}

// ConfigureEnvironment configures the environment/logs mapping.
func (c *Client) ConfigureEnvironment(ctx context.Context, cfg EnvironmentConfig) error {
	return c.postJSON(ctx, environmentPath, cfg, nil)
}

// AddServiceDashboard configures the service dashboard settings.
func (c *Client) AddServiceDashboard(ctx context.Context, cfg ServiceDashboardConfig) error {
	return c.postJSON(ctx, pluginResourcePath+"/asserts/api-server/v1/config/dashboard/Service", cfg, nil)
}

// ConfigureKPIDisplay configures the KPI drawer display settings.
func (c *Client) ConfigureKPIDisplay(ctx context.Context, cfg *KPIDisplayConfig) error {
	return c.postJSON(ctx, pluginResourcePath+"/asserts/api-server/v1/config/display/kpi", cfg, nil)
}

// UploadFrontendO11yRules uploads Frontend Observability recording rules.
func (c *Client) UploadFrontendO11yRules(ctx context.Context, rules *FrontendO11yRuleGroup) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.host+"/api/ruler/grafanacloud-logs/api/v1/rules/frontend-observability?subtype=mimir", nil)
	if err != nil {
		return fmt.Errorf("kg: create request: %w", err)
	}

	b, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("kg: marshal rules: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return readError(resp)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Entity operations
// ---------------------------------------------------------------------------

// GetEntityInfo retrieves rich entity information by type, name, and optional scope.
func (c *Client) GetEntityInfo(ctx context.Context, entityType, name string, scope map[string]string, startMs, endMs int64) (*GraphEntity, error) {
	if startMs == 0 || endMs == 0 {
		endMs = time.Now().UnixMilli()
		startMs = endMs - 3600000
	}
	q := url.Values{}
	q.Set("entity_type", entityType)
	q.Set("entity_name", name)
	q.Set("start", strconv.FormatInt(startMs, 10))
	q.Set("end", strconv.FormatInt(endMs, 10))
	for k, v := range scope {
		q.Set(k, v)
	}
	var result GraphEntity
	if err := c.getJSON(ctx, entitiesPath+"?"+q.Encode(), &result); err != nil {
		return nil, fmt.Errorf("kg: get entity info: %w", err)
	}
	return &result, nil
}

// LookupEntity retrieves entity details from Prometheus alert label params.
// Returns nil, nil on 204 No Content (entity not found).
func (c *Client) LookupEntity(ctx context.Context, entityType, name string, scope map[string]string, startMs, endMs int64) (*GraphEntity, error) {
	if startMs == 0 || endMs == 0 {
		endMs = time.Now().UnixMilli()
		startMs = endMs - 3600000
	}
	q := url.Values{}
	q.Set("asserts_entity_type", entityType)
	q.Set("asserts_entity_name", name)
	q.Set("start", strconv.FormatInt(startMs, 10))
	q.Set("end", strconv.FormatInt(endMs, 10))
	for k, v := range scope {
		q.Set(k, v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+entityLookupPath+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("kg: create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil //nolint:nilnil
	}
	if resp.StatusCode >= 400 {
		return nil, readError(resp)
	}
	var result GraphEntity
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("kg: decode entity: %w", err)
	}
	return &result, nil
}

// CountEntityTypes retrieves entity type counts for the last hour.
func (c *Client) CountEntityTypes(ctx context.Context) (map[string]int64, error) {
	now := time.Now()
	body := EntityCountRequest{
		TimeCriteria: &TimeCriteria{
			Start: now.Add(-1 * time.Hour).UnixMilli(),
			End:   now.UnixMilli(),
		},
	}
	var result map[string]int64
	if err := c.postJSON(ctx, entityTypesPath+"/count", body, &result); err != nil {
		return nil, fmt.Errorf("kg: count entity types: %w", err)
	}
	return result, nil
}

// ListEntityScopes retrieves the available scope dimension values.
func (c *Client) ListEntityScopes(ctx context.Context) (map[string][]string, error) {
	var wrapper struct {
		ScopeValues map[string][]string `json:"scopeValues"`
	}
	if err := c.getJSON(ctx, scopesPath, &wrapper); err != nil {
		return nil, fmt.Errorf("kg: list entity scopes: %w", err)
	}
	return wrapper.ScopeValues, nil
}

// ---------------------------------------------------------------------------
// Assertions operations
// ---------------------------------------------------------------------------

// QueryAssertions queries assertions for a given time range and filters.
func (c *Client) QueryAssertions(ctx context.Context, req AssertionsRequest) ([]AssertionTimeline, error) {
	var result []AssertionTimeline
	if err := c.postJSON(ctx, assertionsPath, req, &result); err != nil {
		return nil, fmt.Errorf("kg: query assertions: %w", err)
	}
	if result == nil {
		return []AssertionTimeline{}, nil
	}
	return result, nil
}

// AssertionsSummary returns a summary of assertions for a given time range and filters.
func (c *Client) AssertionsSummary(ctx context.Context, req AssertionsRequest) (*AssertionSummary, error) {
	var result AssertionSummary
	if err := c.postJSON(ctx, assertionsPath+"/summary", req, &result); err != nil {
		return nil, fmt.Errorf("kg: assertions summary: %w", err)
	}
	return &result, nil
}

// AssertionsGraph queries assertions with graph topology.
func (c *Client) AssertionsGraph(ctx context.Context, req AssertionsRequest) (*AssertionsGraphResponse, error) {
	var result AssertionsGraphResponse
	if err := c.postJSON(ctx, assertionsPath+"/graph", req, &result); err != nil {
		return nil, fmt.Errorf("kg: assertions graph: %w", err)
	}
	return &result, nil
}

// AssertionEntityMetric retrieves metric data for a specific assertion on an entity.
func (c *Client) AssertionEntityMetric(ctx context.Context, req EntityMetricRequest) (*EntityMetricResponse, error) {
	var result EntityMetricResponse
	if err := c.postJSON(ctx, assertionsPath+"/entity-metric", req, &result); err != nil {
		return nil, fmt.Errorf("kg: assertion entity metric: %w", err)
	}
	return &result, nil
}

// AssertionSourceMetrics retrieves source metrics for a specific assertion.
func (c *Client) AssertionSourceMetrics(ctx context.Context, req SourceMetricsRequest) ([]SourceMetricsResponse, error) {
	var result []SourceMetricsResponse
	if err := c.postJSON(ctx, pluginResourcePath+"/asserts/api-server/v1/assertion/source-metrics", req, &result); err != nil {
		return nil, fmt.Errorf("kg: assertion source metrics: %w", err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Search operations
// ---------------------------------------------------------------------------

// Search searches for entities matching the given request.
func (c *Client) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	var wrapper struct {
		Data struct {
			Entities []SearchResult `json:"entities"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, searchPath, req, &wrapper); err != nil {
		return nil, fmt.Errorf("kg: search: %w", err)
	}
	if wrapper.Data.Entities == nil {
		return []SearchResult{}, nil
	}
	return wrapper.Data.Entities, nil
}

// SearchAssertions searches for assertion timelines matching the given query.
func (c *Client) SearchAssertions(ctx context.Context, req SearchRequest) ([]AssertionTimeline, error) {
	var result []AssertionTimeline
	if err := c.postJSON(ctx, searchPath+"/assertions", req, &result); err != nil {
		return nil, fmt.Errorf("kg: search assertions: %w", err)
	}
	if result == nil {
		return []AssertionTimeline{}, nil
	}
	return result, nil
}

// SearchSample returns a sample of search results.
func (c *Client) SearchSample(ctx context.Context, req SampleSearchRequest) ([]SearchResult, error) {
	var wrapper struct {
		Entities []SearchResult `json:"entities"`
	}
	if err := c.postJSON(ctx, searchPath+"/sample", req, &wrapper); err != nil {
		return nil, fmt.Errorf("kg: search sample: %w", err)
	}
	if wrapper.Entities == nil {
		return []SearchResult{}, nil
	}
	return wrapper.Entities, nil
}

// ---------------------------------------------------------------------------
// Rules operations
// ---------------------------------------------------------------------------

// ListRules retrieves all Asserts prom rules.
func (c *Client) ListRules(ctx context.Context) ([]Rule, error) {
	var wrapper struct {
		Rules []Rule `json:"rules"`
	}
	if err := c.getJSON(ctx, rulesPath, &wrapper); err != nil {
		return nil, fmt.Errorf("kg: list rules: %w", err)
	}
	if wrapper.Rules == nil {
		return []Rule{}, nil
	}
	return wrapper.Rules, nil
}

// GetRule retrieves a specific Asserts prom rule by name.
func (c *Client) GetRule(ctx context.Context, name string) (*Rule, error) {
	var wrapper struct {
		Rules []Rule `json:"rules"`
	}
	if err := c.getJSON(ctx, rulesPath+name, &wrapper); err != nil {
		return nil, fmt.Errorf("kg: get rule %q: %w", name, err)
	}
	if len(wrapper.Rules) == 0 {
		return nil, fmt.Errorf("kg: rule %q not found", name)
	}
	return &wrapper.Rules[0], nil
}

// ---------------------------------------------------------------------------
// Graph display config
// ---------------------------------------------------------------------------

// GetGraphDisplayConfig retrieves the graph display configuration.
func (c *Client) GetGraphDisplayConfig(ctx context.Context) (*GraphDisplayConfig, error) {
	var result GraphDisplayConfig
	if err := c.getJSON(ctx, graphDisplayPath, &result); err != nil {
		return nil, fmt.Errorf("kg: get graph display config: %w", err)
	}
	return &result, nil
}
