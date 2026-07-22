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

	"github.com/grafana/gcx/internal/config"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/rest"
)

// ruleFetchConcurrency caps parallel GetRule calls during ListRules fan-out.
const ruleFetchConcurrency = 10

const pluginResourcePath = "/api/plugins/grafana-asserts-app/resources"

const (
	statusPath               = pluginResourcePath + "/asserts/api-server/v1/stack/status"
	entitiesPath             = pluginResourcePath + "/asserts/api-server/v1/entity/info"
	entityTypesPath          = pluginResourcePath + "/asserts/api-server/v1/entity_type"
	entityCountPath          = entityTypesPath + "/count"
	scopesPath               = pluginResourcePath + "/asserts/api-server/v1/entity_scope"
	assertionsPath           = pluginResourcePath + "/asserts/api-server/v1/assertions"
	assertMetricPath         = assertionsPath + "/entity-metric"
	assertLLMPath            = assertionsPath + "/llm-summary"
	sourceMetricPath         = pluginResourcePath + "/asserts/api-server/v1/assertion/source-metrics"
	searchPath               = pluginResourcePath + "/asserts/api-server/v1/search"
	searchAssertPath         = searchPath + "/assertions"
	searchSamplePath         = searchPath + "/sample"
	rulesPath                = pluginResourcePath + "/asserts/api-server/v1/config/prom-rules"
	ruleByNameFmt            = rulesPath + "/%s"
	rulesSchemaPath          = rulesPath + "/schema"
	modelRulesPath           = pluginResourcePath + "/asserts/api-server/v1/config/model-rules"
	modelRulesByNameFmt      = modelRulesPath + "/%s"
	modelRulesSchemaPath     = modelRulesPath + "/schema"
	suppressionPath          = pluginResourcePath + "/asserts/api-server/v1/config/disabled-alert"
	suppressionByNameFmt     = suppressionPath + "/%s"
	suppressionsPath         = pluginResourcePath + "/asserts/api-server/v1/config/disabled-alerts"
	suppressionsValidatePath = suppressionsPath + "-validate"
	entityLookupPath         = pluginResourcePath + "/asserts/api-server/v1/entity"
	v2ConfigPath             = pluginResourcePath + "/asserts/api-server/v2/config"
	v2LogConfigPath          = v2ConfigPath + "/log"
	v2TraceConfigPath        = v2ConfigPath + "/trace"
	v2ProfileConfigPath      = v2ConfigPath + "/profile"
	v2RelabelRulesPath       = v2ConfigPath + "/relabel-rules"

	// KG Write API (K8s-style, namespaced). %s is the stacks-<id> namespace.
	// Entity and relationship deletes use these collection paths with identity
	// in the query string (type/name are not path segments), so names containing
	// '/' are addressable without Tomcat rejecting encoded solidus.
	//
	// Routed through the asserts plugin's resource proxy: the plugin backend
	// forwards unmatched resource paths to the asserts cell gateway with the
	// stack's Basic auth, and the gateway maps /apis/kg.grafana.com/* to the
	// KG api-server. The bare /apis/kg.grafana.com group is not registered
	// with Grafana's API aggregator, so it is not reachable on the stack URL
	// directly.
	kgWriteAPIBase     = pluginResourcePath + "/apis/kg.grafana.com/v1alpha1/namespaces/%s"
	kgEntitiesPathFmt  = kgWriteAPIBase + "/entities"
	kgRelationshipsFmt = kgWriteAPIBase + "/relationships"
)

// RelabelRuleType identifies which Mimir relabel rule group to operate on.
type RelabelRuleType string

const (
	RelabelRuleTypePrologue  RelabelRuleType = "prologue"
	RelabelRuleTypeEpilogue  RelabelRuleType = "epilogue"
	RelabelRuleTypeGenerated RelabelRuleType = "generated"
)

// IsValid reports whether t is one of the known relabel rule types.
func (t RelabelRuleType) IsValid() bool {
	switch t {
	case RelabelRuleTypePrologue, RelabelRuleTypeEpilogue, RelabelRuleTypeGenerated:
		return true
	}
	return false
}

// Client is an HTTP client for the Knowledge Graph (Asserts) API.
type Client struct {
	httpClient *http.Client
	host       string
	namespace  string
}

// NewClient creates a new KG client from the given REST config.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("kg: failed to create HTTP client: %w", err)
	}
	return &Client{httpClient: httpClient, host: cfg.Host, namespace: cfg.Namespace}, nil
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

// doJSONStatus performs a JSON request and returns the HTTP status code so
// callers can distinguish 201-created from 200-updated. Decodes into v if non-nil.
func (c *Client) doJSONStatus(ctx context.Context, method, path string, body, v any) (int, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("kg: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("kg: create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return resp.StatusCode, readError(resp)
	}
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return resp.StatusCode, fmt.Errorf("kg: decode response: %w", err)
		}
	}
	return resp.StatusCode, nil
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

func (e *APIError) HTTPStatusCode() int {
	return e.StatusCode
}

func (e *APIError) APIServiceName() string {
	return "Knowledge Graph"
}

func (e *APIError) APIUserMessage() string {
	if e.message != "" {
		return e.message
	}
	return e.rawBody
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
// Stack status
// ---------------------------------------------------------------------------

// GetStatus retrieves the current Knowledge Graph status.
func (c *Client) GetStatus(ctx context.Context) (*Status, error) {
	var status Status
	if err := c.getJSON(ctx, statusPath, &status); err != nil {
		return nil, fmt.Errorf("kg: get status: %w", err)
	}
	return &status, nil
}

// ---------------------------------------------------------------------------
// Configuration upload operations
// ---------------------------------------------------------------------------

// UploadPromRules uploads Prometheus recording rules.
func (c *Client) UploadPromRules(ctx context.Context, yamlContent string) error {
	return c.doYAML(ctx, http.MethodPut, rulesPath, yamlContent)
}

// GetPromRulesSchema fetches the live JSON Schema for the Prometheus rules config from the backend.
//
// Backend caches this for 1h. The schema is derived from the Java DTO tree, so it
// reflects the authoritative prom-rules shape — use it to validate prom-rules YAML
// before uploading instead of learning the shape from 400s.
func (c *Client) GetPromRulesSchema(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	if err := c.getJSON(ctx, rulesSchemaPath, &result); err != nil {
		return nil, fmt.Errorf("kg: get prom rules schema: %w", err)
	}
	return result, nil
}

// UploadModelRules uploads model rules configuration.
func (c *Client) UploadModelRules(ctx context.Context, yamlContent string) error {
	return c.doYAML(ctx, http.MethodPut, modelRulesPath, yamlContent)
}

// ListModelRuleNames returns the names of all custom model rule configurations for the tenant.
func (c *Client) ListModelRuleNames(ctx context.Context) ([]string, error) {
	var result ModelRuleNames
	if err := c.getJSON(ctx, modelRulesPath, &result); err != nil {
		return nil, fmt.Errorf("kg: list model rules: %w", err)
	}
	return result.RuleNames, nil
}

// ListModelRules retrieves all custom model rule configurations for the tenant.
//
// The backend list endpoint only returns names, so this performs an N+1
// fan-out: one call to list names, then bounded-parallel GetModelRules per name.
func (c *Client) ListModelRules(ctx context.Context) ([]ModelRules, error) {
	names, err := c.ListModelRuleNames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModelRules, len(names))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(ruleFetchConcurrency)
	for i, name := range names {
		g.Go(func() error {
			m, err := c.GetModelRules(gCtx, name)
			if err != nil {
				return err
			}
			out[i] = *m
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("kg: list model rules: %w", err)
	}
	return out, nil
}

// GetModelRules retrieves a single custom model rules configuration by name.
func (c *Client) GetModelRules(ctx context.Context, name string) (*ModelRules, error) {
	var result ModelRules
	path := fmt.Sprintf(modelRulesByNameFmt, url.PathEscape(name))
	if err := c.getJSON(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("kg: get model rules %q: %w", name, err)
	}
	return &result, nil
}

// GetModelRulesSchema fetches the live JSON Schema for the ModelRulesDto from the backend.
//
// Backend caches this for 1h. The schema is derived from the Java DTO tree, so it
// reflects the full nested shape (entities, relations, enrichment, etc.) — much
// deeper than the shallow schema embedded in the gcx binary.
func (c *Client) GetModelRulesSchema(ctx context.Context) (map[string]any, error) {
	var result map[string]any
	if err := c.getJSON(ctx, modelRulesSchemaPath, &result); err != nil {
		return nil, fmt.Errorf("kg: get model rules schema: %w", err)
	}
	return result, nil
}

// DeleteModelRules deletes a custom model rules configuration by name.
func (c *Client) DeleteModelRules(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.host+fmt.Sprintf(modelRulesByNameFmt, url.PathEscape(name)), nil)
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
	return nil
}

// Suppression represents a single disabled-alert configuration entry.
type Suppression struct {
	Name        string            `json:"name" yaml:"name"`
	MatchLabels map[string]string `json:"matchLabels,omitempty" yaml:"matchLabels,omitempty"`
	ManagedBy   string            `json:"managedBy,omitempty" yaml:"managedBy,omitempty"`
}

// Suppressions is the batch payload shape returned by GET /v1/config/disabled-alerts.
type Suppressions struct {
	DisabledAlertConfigs []Suppression `json:"disabledAlertConfigs" yaml:"disabledAlertConfigs"`
}

// UpsertSuppression creates or updates a single suppression without affecting others.
// It uses the single-item endpoint so the backend performs a read-modify-write upsert.
func (c *Client) UpsertSuppression(ctx context.Context, s Suppression) error {
	return c.postJSON(ctx, suppressionPath, s, nil)
}

// DeleteSuppression deletes a single suppression by name.
func (c *Client) DeleteSuppression(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.host+fmt.Sprintf(suppressionByNameFmt, url.PathEscape(name)), nil)
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
	return nil
}

// GetSuppressions retrieves all disabled-alert configurations for the tenant.
func (c *Client) GetSuppressions(ctx context.Context) (*Suppressions, error) {
	var result Suppressions
	if err := c.getJSON(ctx, suppressionsPath, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SuppressionFieldError is a single field-level validation failure reported by
// the disabled-alerts-validate endpoint, e.g. field "disabledAlertConfigs[0].name",
// message "Missing Rule Name".
type SuppressionFieldError struct {
	Field   string
	Message string
}

// SuppressionsValidationError is returned by ValidateSuppressions when the
// backend rejects the payload (HTTP 422). It carries the per-field errors from
// the server-side validator so callers can render them without a round-trip to
// the write endpoint.
type SuppressionsValidationError struct {
	StatusCode int
	Message    string
	Errors     []SuppressionFieldError
}

func (e *SuppressionsValidationError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = "suppressions configuration is invalid"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "kg: %s", msg)
	for _, fe := range e.Errors {
		fmt.Fprintf(&b, "\n  - %s: %s", fe.Field, fe.Message)
	}
	return b.String()
}

// HTTPStatusCode implements the interface used by internal/fail for rich errors.
func (e *SuppressionsValidationError) HTTPStatusCode() int { return e.StatusCode }

// APIServiceName implements the interface used by internal/fail for rich errors.
func (e *SuppressionsValidationError) APIServiceName() string { return "Knowledge Graph" }

// APIUserMessage implements the interface used by internal/fail for rich errors.
func (e *SuppressionsValidationError) APIUserMessage() string { return e.Error() }

// ValidateSuppressions checks a suppression configuration against the backend's
// server-side validator without persisting anything, via the validate-only
// endpoint. It returns nil when the payload is valid, a
// *SuppressionsValidationError on HTTP 422 (with per-field errors), or an
// *APIError for other failures.
func (c *Client) ValidateSuppressions(ctx context.Context, s *Suppressions) error {
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("kg: marshal request body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+suppressionsValidatePath, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("kg: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("kg: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnprocessableEntity {
		return parseSuppressionsValidationError(resp)
	}
	if resp.StatusCode >= 400 {
		return readError(resp)
	}
	return nil
}

// parseSuppressionsValidationError parses the ApiError envelope returned by the
// validate endpoint on HTTP 422 (fields message + subErrors[]{field,message}).
// If the body is not the expected envelope, it falls back to a generic APIError.
func parseSuppressionsValidationError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &APIError{StatusCode: resp.StatusCode}
	}
	var env struct {
		Message   string `json:"message"`
		SubErrors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"subErrors"`
	}
	if jsonErr := json.Unmarshal(body, &env); jsonErr == nil && len(env.SubErrors) > 0 {
		verr := &SuppressionsValidationError{StatusCode: resp.StatusCode, Message: env.Message}
		for _, se := range env.SubErrors {
			verr.Errors = append(verr.Errors, SuppressionFieldError{Field: se.Field, Message: se.Message})
		}
		return verr
	}
	// Not the expected validation envelope — fall back to generic parsing.
	apiErr := &APIError{StatusCode: resp.StatusCode}
	if env.Message != "" {
		apiErr.message = env.Message
	} else if len(body) > 0 {
		apiErr.rawBody = string(body)
	}
	return apiErr
}

// GetRelabelRules fetches the relabel rule group of the given type.
// Returns nil, nil when the backend responds with 204 No Content (no rules configured).
func (c *Client) GetRelabelRules(ctx context.Context, t RelabelRuleType) (map[string]any, error) {
	if !t.IsValid() {
		return nil, fmt.Errorf("kg: invalid relabel rule type %q", t)
	}
	path := v2RelabelRulesPath + "/" + string(t)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("kg: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

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
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("kg: decode relabel rules: %w", err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Entity operations
// ---------------------------------------------------------------------------

// timeRangeParams returns query params with start/end set for the given window,
// defaulting to the last hour when either bound is unset.
func timeRangeParams(startMs, endMs int64) url.Values {
	if startMs == 0 || endMs == 0 {
		endMs = time.Now().UnixMilli()
		startMs = endMs - 3600000
	}
	q := url.Values{}
	q.Set("start", strconv.FormatInt(startMs, 10))
	q.Set("end", strconv.FormatInt(endMs, 10))
	return q
}

// GetEntityInfo retrieves rich entity information by type, name, and optional scope.
func (c *Client) GetEntityInfo(ctx context.Context, entityType, name string, scope map[string]string, domain string, startMs, endMs int64) (*GraphEntity, error) {
	q := timeRangeParams(startMs, endMs)
	q.Set("entity_type", entityType)
	q.Set("entity_name", name)
	for k, v := range scope {
		q.Set(k, v)
	}
	if domain != "" {
		q.Set("domain", domain)
	}
	var result GraphEntity
	if err := c.getJSON(ctx, entitiesPath+"?"+q.Encode(), &result); err != nil {
		return nil, fmt.Errorf("kg: get entity info: %w", err)
	}
	return &result, nil
}

// LookupEntity retrieves entity details from Prometheus alert label params.
// Returns nil, nil on 204 No Content (entity not found).
func (c *Client) LookupEntity(ctx context.Context, entityType, name string, scope map[string]string, domain string, startMs, endMs int64) (*GraphEntity, error) {
	q := timeRangeParams(startMs, endMs)
	q.Set("asserts_entity_type", entityType)
	q.Set("asserts_entity_name", name)
	for k, v := range scope {
		q.Set(k, v)
	}
	if domain != "" {
		q.Set("domain", domain)
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

// CountEntityTypes retrieves entity type counts for the given time window and scope.
func (c *Client) CountEntityTypes(ctx context.Context, startMs, endMs int64, sc *ScopeCriteria) (map[string]int64, error) {
	body := EntityCountRequest{
		TimeCriteria:  &TimeCriteria{Start: startMs, End: endMs},
		ScopeCriteria: sc,
	}
	var result map[string]int64
	if err := c.postJSON(ctx, entityCountPath, body, &result); err != nil {
		return nil, fmt.Errorf("kg: count entity types: %w", err)
	}
	return result, nil
}

// ListEntityScopes retrieves the available scope dimension values.
func (c *Client) ListEntityScopes(ctx context.Context, startMs, endMs int64) (map[string][]string, error) {
	q := timeRangeParams(startMs, endMs)
	var wrapper struct {
		ScopeValues map[string][]string `json:"scopeValues"`
	}
	if err := c.getJSON(ctx, scopesPath+"?"+q.Encode(), &wrapper); err != nil {
		return nil, fmt.Errorf("kg: list entity scopes: %w", err)
	}
	return wrapper.ScopeValues, nil
}

// ---------------------------------------------------------------------------
// Assertions operations
// ---------------------------------------------------------------------------

// AssertionEntityMetric retrieves metric data for a specific assertion on an entity.
func (c *Client) AssertionEntityMetric(ctx context.Context, req EntityMetricRequest) (*EntityMetricResponse, error) {
	var result EntityMetricResponse
	if err := c.postJSON(ctx, assertMetricPath, req, &result); err != nil {
		return nil, fmt.Errorf("kg: assertion entity metric: %w", err)
	}
	return &result, nil
}

// AssertionSourceMetrics retrieves source metrics for a specific assertion.
func (c *Client) AssertionSourceMetrics(ctx context.Context, req SourceMetricsRequest) ([]SourceMetricsResponse, error) {
	var result []SourceMetricsResponse
	if err := c.postJSON(ctx, sourceMetricPath, req, &result); err != nil {
		return nil, fmt.Errorf("kg: assertion source metrics: %w", err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Search operations
// ---------------------------------------------------------------------------

// SearchPage is one page of entity search results plus the backend's
// pagination signals. LastPage is true when no further pages exist;
// MaxLimitHit is true when the backend's per-page result cap was reached
// (i.e. the page is truncated and a subsequent --page would return more).
type SearchPage struct {
	Entities    []SearchResult
	PageNum     int
	LastPage    bool
	MaxLimitHit bool
}

// Search searches for entities matching the given request.
func (c *Client) Search(ctx context.Context, req SearchRequest) (SearchPage, error) {
	var wrapper struct {
		Data struct {
			Entities                 []SearchResult `json:"entities"`
			PageNum                  int            `json:"pageNum"`
			LastPage                 bool           `json:"lastPage"`
			SearchResultsMaxLimitHit bool           `json:"searchResultsMaxLimitHit"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, searchPath, req, &wrapper); err != nil {
		return SearchPage{}, fmt.Errorf("kg: search: %w", err)
	}
	entities := wrapper.Data.Entities
	if entities == nil {
		entities = []SearchResult{}
	}
	return SearchPage{
		Entities:    entities,
		PageNum:     wrapper.Data.PageNum,
		LastPage:    wrapper.Data.LastPage,
		MaxLimitHit: wrapper.Data.SearchResultsMaxLimitHit,
	}, nil
}

// SearchAssertions searches for assertion timelines matching the given query.
func (c *Client) SearchAssertions(ctx context.Context, req SearchRequest) ([]AssertionTimeline, error) {
	var result []AssertionTimeline
	if err := c.postJSON(ctx, searchAssertPath, req, &result); err != nil {
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
	if err := c.postJSON(ctx, searchSamplePath, req, &wrapper); err != nil {
		return nil, fmt.Errorf("kg: search sample: %w", err)
	}
	if wrapper.Entities == nil {
		return []SearchResult{}, nil
	}
	return wrapper.Entities, nil
}

// FetchGraphSchema fetches the Knowledge Graph schema using the special schema search definition.
// The response entities represent entity types (not instances); their Name is the type name.
func (c *Client) FetchGraphSchema(ctx context.Context, startMs, endMs int64) (GraphSchemaResponse, error) {
	body := map[string]any{
		"definitionId": 6,
		"bindings": map[string]any{
			"boundTag": "Show customer schema",
			"updated":  endMs,
		},
		"timeCriteria": map[string]any{
			"start": startMs,
			"end":   endMs,
		},
	}
	var resp GraphSchemaResponse
	if err := c.postJSON(ctx, searchPath, body, &resp); err != nil {
		return GraphSchemaResponse{}, fmt.Errorf("kg: fetch schema: %w", err)
	}
	return resp, nil
}

// FetchLogConfigs fetches log drilldown configs from the v2 API.
func (c *Client) FetchLogConfigs(ctx context.Context) (LogConfigsResponse, error) {
	var resp LogConfigsResponse
	if err := c.getJSON(ctx, v2LogConfigPath, &resp); err != nil {
		return LogConfigsResponse{}, fmt.Errorf("kg: fetch log configs: %w", err)
	}
	return resp, nil
}

// FetchTraceConfigs fetches trace drilldown configs from the v2 API.
func (c *Client) FetchTraceConfigs(ctx context.Context) (TraceConfigsResponse, error) {
	var resp TraceConfigsResponse
	if err := c.getJSON(ctx, v2TraceConfigPath, &resp); err != nil {
		return TraceConfigsResponse{}, fmt.Errorf("kg: fetch trace configs: %w", err)
	}
	return resp, nil
}

// FetchProfileConfigs fetches profile drilldown configs from the v2 API.
func (c *Client) FetchProfileConfigs(ctx context.Context) (ProfileConfigsResponse, error) {
	var resp ProfileConfigsResponse
	if err := c.getJSON(ctx, v2ProfileConfigPath, &resp); err != nil {
		return ProfileConfigsResponse{}, fmt.Errorf("kg: fetch profile configs: %w", err)
	}
	return resp, nil
}

// CypherSearch runs a read-only Cypher query against the Knowledge Graph.
func (c *Client) CypherSearch(ctx context.Context, req CypherSearchRequest) (*CypherSearchResponse, error) {
	var result CypherSearchResponse
	if err := c.postJSON(ctx, searchPath+"/cypher", req, &result); err != nil {
		return nil, fmt.Errorf("kg: cypher search: %w", err)
	}
	if result.Entities == nil {
		result.Entities = []CypherEntity{}
	}
	if result.Edges == nil {
		result.Edges = []CypherEdge{}
	}
	return &result, nil
}

// LLMSummary fetches entity health data from the LLM summary endpoint.
func (c *Client) LLMSummary(ctx context.Context, req LLMSummaryRequest) (map[string]any, error) {
	var result map[string]any
	if err := c.postJSON(ctx, assertLLMPath, req, &result); err != nil {
		return nil, fmt.Errorf("kg: llm summary: %w", err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Rules operations
// ---------------------------------------------------------------------------

// ListRuleNames retrieves the names of all Asserts prom rules.
//
// Backend: GET /v1/config/prom-rules → {"ruleNames": [...]}.
func (c *Client) ListRuleNames(ctx context.Context) ([]string, error) {
	var wrapper struct {
		RuleNames []string `json:"ruleNames"`
	}
	if err := c.getJSON(ctx, rulesPath, &wrapper); err != nil {
		return nil, fmt.Errorf("kg: list rule names: %w", err)
	}
	return wrapper.RuleNames, nil
}

// ListRules retrieves all Asserts prom rules.
//
// The backend list endpoint only returns names, so this performs an N+1
// fan-out: one call to list names, then bounded-parallel GetRule per name.
func (c *Client) ListRules(ctx context.Context) ([]Rule, error) {
	names, err := c.ListRuleNames(ctx)
	if err != nil {
		return nil, err
	}
	files := make([]Rule, len(names))
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(ruleFetchConcurrency)
	for i, name := range names {
		g.Go(func() error {
			f, err := c.GetRule(gCtx, name)
			if err != nil {
				return err
			}
			files[i] = *f
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("kg: list rules: %w", err)
	}
	return files, nil
}

// DeleteRule deletes a single Asserts prom rule by name.
//
// Backend: DELETE /v1/config/prom-rules/{name}.
func (c *Client) DeleteRule(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.host+fmt.Sprintf(ruleByNameFmt, url.PathEscape(name)), nil)
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
	return nil
}

// GetRule retrieves a specific Asserts prom rule by name.
//
// Backend: GET /v1/config/prom-rules/{name} → PrometheusRulesDto (no wrapper).
// Some backend versions reply 200 with an empty payload for a missing rule
// instead of 404, so we treat an empty body as not-found.
func (c *Client) GetRule(ctx context.Context, name string) (*Rule, error) {
	var f Rule
	if err := c.getJSON(ctx, fmt.Sprintf(ruleByNameFmt, url.PathEscape(name)), &f); err != nil {
		return nil, fmt.Errorf("kg: get rule %q: %w", name, err)
	}
	if f.Name == "" && len(f.Groups) == 0 {
		return nil, fmt.Errorf("kg: rule %q not found", name)
	}
	return &f, nil
}

// ---------------------------------------------------------------------------
// KG Write API: relationships
// ---------------------------------------------------------------------------

// UpsertRelationship creates or updates a custom (API-origin) edge between two
// existing entities. Both endpoints must already exist (404 otherwise).
func (c *Client) UpsertRelationship(ctx context.Context, req RelationshipWriteRequest) (*RelationshipWriteResponse, error) {
	path := fmt.Sprintf(kgRelationshipsFmt, url.PathEscape(c.namespace))
	var resp RelationshipWriteResponse
	if _, err := c.doJSONStatus(ctx, http.MethodPost, path, req, &resp); err != nil {
		return nil, fmt.Errorf("kg: upsert relationship: %w", err)
	}
	return &resp, nil
}

// DeleteRelationship deletes a custom edge of relType between from and to.
// Identity travels as query params on the collection path (type, from.*, to.*);
// there is no request body.
func (c *Client) DeleteRelationship(ctx context.Context, relType string, from, to EntityRef) error {
	path := fmt.Sprintf(kgRelationshipsFmt, url.PathEscape(c.namespace))
	q := url.Values{}
	q.Set("type", relType)
	addRef := func(prefix string, ref EntityRef) {
		q.Set(prefix+".domain", ref.Domain)
		q.Set(prefix+".type", ref.Type)
		q.Set(prefix+".name", ref.Name)
		for k, v := range ref.Scope {
			q.Set(prefix+".scope["+k+"]", v)
		}
	}
	addRef("from", from)
	addRef("to", to)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.host+path+"?"+q.Encode(), nil)
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
	return nil
}

// ---------------------------------------------------------------------------
// KG Write API: entities
// ---------------------------------------------------------------------------

// UpsertEntity creates or updates a custom (API-origin) entity. The returned
// bool is true when the entity was created (HTTP 201), false when updated (200).
func (c *Client) UpsertEntity(ctx context.Context, req EntityWriteRequest) (*EntityWriteResponse, bool, error) {
	path := fmt.Sprintf(kgEntitiesPathFmt, url.PathEscape(c.namespace))
	var resp EntityWriteResponse
	status, err := c.doJSONStatus(ctx, http.MethodPost, path, req, &resp)
	if err != nil {
		return nil, false, fmt.Errorf("kg: upsert entity: %w", err)
	}
	return &resp, status == http.StatusCreated, nil
}

// DeleteEntity deletes a custom entity identified by (domain, type, name, scope).
// Identity travels as query params on the collection path (domain, type, name,
// and optional scope[key]=value); there is no request body. Scope is
// identity-significant and must match the value used at upsert.
func (c *Client) DeleteEntity(ctx context.Context, domain, entityType, name string, scope map[string]string) error {
	path := fmt.Sprintf(kgEntitiesPathFmt, url.PathEscape(c.namespace))
	q := url.Values{}
	q.Set("domain", domain)
	q.Set("type", entityType)
	q.Set("name", name)
	for k, v := range scope {
		q.Set("scope["+k+"]", v)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.host+path+"?"+q.Encode(), nil)
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
	return nil
}
