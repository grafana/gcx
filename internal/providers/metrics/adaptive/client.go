package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// serverError trims trailing whitespace from server response bodies so that
// error messages don't contain stray newlines in formatted output.
func serverError(b []byte) string {
	return strings.TrimRight(string(b), " \t\r\n")
}

// Sentinel errors for specific HTTP status codes.
var (
	ErrRuleNotFound       = fmt.Errorf("rule: %w", adapter.ErrNotFound)
	ErrPreconditionFailed = errors.New("rule was modified concurrently")
	ErrSegmentNotFound    = fmt.Errorf("segment: %w", adapter.ErrNotFound)
	ErrExemptionNotFound  = fmt.Errorf("exemption: %w", adapter.ErrNotFound)
)

// Client is an HTTP client for the Grafana Adaptive Metrics API.
type Client struct {
	baseURL    string
	tenantID   int
	apiToken   string
	httpClient *http.Client
}

// NewClient creates a new Adaptive Metrics client.
// If httpClient is nil, httputils.NewDefaultClient is used.
func NewClient(ctx context.Context, baseURL string, tenantID int, apiToken string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = httputils.NewDefaultClient(ctx)
	}
	return &Client{
		baseURL:    baseURL,
		tenantID:   tenantID,
		apiToken:   apiToken,
		httpClient: httpClient,
	}
}

// doRequest builds and executes an HTTP request against the Adaptive Metrics API.
func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: create request: %w", err)
	}
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return c.httpClient.Do(req)
}

// ---------------------------------------------------------------------------
// Segments
// ---------------------------------------------------------------------------

// ListSegments returns all Adaptive Metrics segments.
func (c *Client) ListSegments(ctx context.Context) ([]MetricSegment, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/aggregations/rules/segments", nil)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: list segments: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: list segments: status %d: %s", resp.StatusCode, serverError(b))
	}

	var segments []MetricSegment
	if err := json.NewDecoder(resp.Body).Decode(&segments); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: list segments: decode: %w", err)
	}

	if segments == nil {
		return []MetricSegment{}, nil
	}

	return segments, nil
}

// GetSegment returns a single segment by ID (list + filter, no server-side get-by-id).
// Returns ErrSegmentNotFound if not found.
func (c *Client) GetSegment(ctx context.Context, id string) (*MetricSegment, error) {
	segments, err := c.ListSegments(ctx)
	if err != nil {
		return nil, err
	}

	for i := range segments {
		if segments[i].ID == id {
			return &segments[i], nil
		}
	}

	return nil, ErrSegmentNotFound
}

// CreateSegment creates a new Adaptive Metrics segment.
func (c *Client) CreateSegment(ctx context.Context, s *MetricSegment) (*MetricSegment, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: create segment: marshal: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/aggregations/rules/segments", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: create segment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: create segment: status %d: %s", resp.StatusCode, serverError(b))
	}

	var created MetricSegment
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: create segment: decode: %w", err)
	}

	return &created, nil
}

// UpdateSegment updates an existing segment. The server returns an empty body on success,
// so the input segment (with ID set) is returned.
func (c *Client) UpdateSegment(ctx context.Context, id string, s *MetricSegment) (*MetricSegment, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: update segment: marshal: %w", err)
	}

	path := "/aggregations/rules/segments?segment=" + url.QueryEscape(id)
	resp, err := c.doRequest(ctx, http.MethodPut, path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: update segment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrSegmentNotFound
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: update segment: status %d: %s", resp.StatusCode, serverError(b))
	}

	result := *s
	result.ID = id
	return &result, nil
}

// DeleteSegment deletes a segment by ID. Returns 409 if the segment has dependent data.
func (c *Client) DeleteSegment(ctx context.Context, id string) error {
	path := "/aggregations/rules/segments?segment=" + url.QueryEscape(id)
	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("adaptive-metrics: delete segment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("adaptive-metrics: delete segment: segment has dependent rules or exemptions: %s", string(b))
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("adaptive-metrics: delete segment: status %d: %s", resp.StatusCode, serverError(b))
	}

	return nil
}

// ---------------------------------------------------------------------------
// Exemptions
// ---------------------------------------------------------------------------

// ListExemptions returns all exemptions, optionally scoped to a segment.
// The API wraps results in {"result": [...]}.
func (c *Client) ListExemptions(ctx context.Context, segment string) ([]MetricExemption, error) {
	path := "/v1/recommendations/exemptions"
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: list exemptions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: list exemptions: status %d: %s", resp.StatusCode, serverError(b))
	}

	var wrapper struct {
		Result []MetricExemption `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: list exemptions: decode: %w", err)
	}

	if wrapper.Result == nil {
		return []MetricExemption{}, nil
	}

	return wrapper.Result, nil
}

// ListSegmentedExemptions returns all exemptions grouped by segment.
func (c *Client) ListSegmentedExemptions(ctx context.Context) ([]ExemptionsBySegmentEntry, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/v1/recommendations/segmented_exemptions", nil)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: list segmented exemptions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: list segmented exemptions: status %d: %s", resp.StatusCode, serverError(b))
	}

	var entries []ExemptionsBySegmentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: list segmented exemptions: decode: %w", err)
	}

	if entries == nil {
		return []ExemptionsBySegmentEntry{}, nil
	}

	return entries, nil
}

// GetExemption returns a single exemption by ID, optionally scoped to a segment.
// Returns ErrExemptionNotFound on 404.
func (c *Client) GetExemption(ctx context.Context, id, segment string) (*MetricExemption, error) {
	path := "/v1/recommendations/exemptions/" + url.PathEscape(id)
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: get exemption: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrExemptionNotFound
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: get exemption: status %d: %s", resp.StatusCode, serverError(b))
	}

	var wrapper struct {
		Result *MetricExemption `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: get exemption: decode: %w", err)
	}
	if wrapper.Result == nil {
		return nil, errors.New("adaptive-metrics: get exemption: empty result")
	}

	return wrapper.Result, nil
}

// CreateExemption creates a new exemption, optionally scoped to a segment.
func (c *Client) CreateExemption(ctx context.Context, e *MetricExemption, segment string) (*MetricExemption, error) {
	path := "/v1/recommendations/exemptions"
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: create exemption: marshal: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPost, path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: create exemption: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: create exemption: status %d: %s", resp.StatusCode, serverError(b))
	}

	var wrapper struct {
		Result *MetricExemption `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: create exemption: decode: %w", err)
	}
	if wrapper.Result == nil {
		return nil, errors.New("adaptive-metrics: create exemption: empty result")
	}

	return wrapper.Result, nil
}

// UpdateExemption updates an existing exemption. The server returns an empty body on success,
// so the input exemption (with ID set) is returned.
func (c *Client) UpdateExemption(ctx context.Context, id string, e *MetricExemption, segment string) (*MetricExemption, error) {
	path := "/v1/recommendations/exemptions/" + url.PathEscape(id)
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: update exemption: marshal: %w", err)
	}

	resp, err := c.doRequest(ctx, http.MethodPut, path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: update exemption: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrExemptionNotFound
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: update exemption: status %d: %s", resp.StatusCode, serverError(b))
	}

	result := *e
	result.ID = id
	return &result, nil
}

// DeleteExemption soft-deletes an exemption, optionally scoped to a segment.
func (c *Client) DeleteExemption(ctx context.Context, id, segment string) error {
	path := "/v1/recommendations/exemptions/" + url.PathEscape(id)
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("adaptive-metrics: delete exemption: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("adaptive-metrics: delete exemption: status %d: %s", resp.StatusCode, serverError(b))
	}

	return nil
}

// ListRules returns all aggregation rules and the current ETag.
func (c *Client) ListRules(ctx context.Context, segment string) ([]MetricRule, string, error) {
	path := "/aggregations/rules"
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, "", fmt.Errorf("adaptive-metrics: list rules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("adaptive-metrics: list rules: status %d: %s", resp.StatusCode, serverError(b))
	}

	etag := resp.Header.Get("Etag")

	var rules []MetricRule
	if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
		return nil, "", fmt.Errorf("adaptive-metrics: list rules: decode: %w", err)
	}

	return rules, etag, nil
}

// GetRule returns a single aggregation rule.
// Returns ErrRuleNotFound if the rule does not exist.
// Note: mutations require the global rules ETag from ListRules, not a per-rule ETag.
func (c *Client) GetRule(ctx context.Context, metric, segment string) (MetricRule, error) {
	path := "/aggregations/rule/" + url.PathEscape(metric)
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return MetricRule{}, fmt.Errorf("adaptive-metrics: get rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return MetricRule{}, ErrRuleNotFound
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return MetricRule{}, fmt.Errorf("adaptive-metrics: get rule: status %d: %s", resp.StatusCode, serverError(b))
	}

	var rule MetricRule
	if err := json.NewDecoder(resp.Body).Decode(&rule); err != nil {
		return MetricRule{}, fmt.Errorf("adaptive-metrics: get rule: decode: %w", err)
	}

	return rule, nil
}

// CreateRule creates a new aggregation rule.
// The etag should be the current rules ETag from ListRules — the API requires
// If-Match even for creates against the individual rule endpoint.
func (c *Client) CreateRule(ctx context.Context, rule MetricRule, etag, segment string) (string, error) {
	path := "/aggregations/rule/" + url.PathEscape(rule.Metric)
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	data, err := json.Marshal(rule)
	if err != nil {
		return "", fmt.Errorf("adaptive-metrics: create rule: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("adaptive-metrics: create rule: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("adaptive-metrics: create rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return "", ErrPreconditionFailed
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("adaptive-metrics: create rule: status %d: %s", resp.StatusCode, serverError(b))
	}

	return resp.Header.Get("Etag"), nil
}

// UpdateRule updates an existing aggregation rule using the provided ETag.
// Returns ErrPreconditionFailed on a 412 conflict.
func (c *Client) UpdateRule(ctx context.Context, rule MetricRule, etag, segment string) (string, error) {
	path := "/aggregations/rule/" + url.PathEscape(rule.Metric)
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	data, err := json.Marshal(rule)
	if err != nil {
		return "", fmt.Errorf("adaptive-metrics: update rule: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("adaptive-metrics: update rule: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("adaptive-metrics: update rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return "", ErrPreconditionFailed
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("adaptive-metrics: update rule: status %d: %s", resp.StatusCode, serverError(b))
	}

	return resp.Header.Get("Etag"), nil
}

// DeleteRule deletes an aggregation rule using the provided ETag.
// Returns ErrPreconditionFailed on a 412 conflict.
func (c *Client) DeleteRule(ctx context.Context, metric, etag, segment string) error {
	path := "/aggregations/rule/" + url.PathEscape(metric)
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("adaptive-metrics: delete rule: create request: %w", err)
	}
	req.Header.Set("If-Match", etag)
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("adaptive-metrics: delete rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return ErrPreconditionFailed
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("adaptive-metrics: delete rule: status %d: %s", resp.StatusCode, serverError(b))
	}

	return nil
}

// SyncRules replaces all aggregation rules using the given ETag for optimistic concurrency.
func (c *Client) SyncRules(ctx context.Context, rules []MetricRule, etag, segment string) error {
	path := "/aggregations/rules"
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	data, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("adaptive-metrics: sync rules: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("adaptive-metrics: sync rules: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("adaptive-metrics: sync rules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return ErrPreconditionFailed
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("adaptive-metrics: sync rules: status %d: %s", resp.StatusCode, serverError(b))
	}

	return nil
}

// ValidateRules validates a set of rules against the API.
// Returns a list of validation error strings on failure.
func (c *Client) ValidateRules(ctx context.Context, rules []MetricRule, segment string) ([]string, error) {
	path := "/aggregations/check-rules"
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	data, err := json.Marshal(rules)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: validate rules: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: validate rules: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: validate rules: %w", err)
	}
	defer resp.Body.Close()

	// 200 = valid rules; 400 = validation errors as []string — both are decoded the same way.
	// Other non-2xx/400 statuses are genuine errors.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: validate rules: status %d: %s", resp.StatusCode, serverError(b))
	}

	var errs []string
	if err := json.NewDecoder(resp.Body).Decode(&errs); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: validate rules: decode: %w", err)
	}

	return errs, nil
}

// ListRecommendations returns all metric recommendations with verbose details.
func (c *Client) ListRecommendations(ctx context.Context, segment string, actions []string) ([]MetricRecommendation, error) {
	params := url.Values{}
	params.Set("verbose", "true")
	if segment != "" {
		params.Set("segment", segment)
	}
	for _, a := range actions {
		params.Add("action", a)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/aggregations/recommendations?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("adaptive-metrics: list recommendations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("adaptive-metrics: list recommendations: status %d: %s", resp.StatusCode, serverError(b))
	}

	var recs []MetricRecommendation
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		return nil, fmt.Errorf("adaptive-metrics: list recommendations: decode: %w", err)
	}

	return recs, nil
}

// ListRecommendedRules returns the complete desired rule set from recommendations
// (verbose=false), plus the Rules-Version header value for use as an ETag.
func (c *Client) ListRecommendedRules(ctx context.Context, segment string) ([]MetricRule, string, error) {
	params := url.Values{}
	params.Set("verbose", "false")
	if segment != "" {
		params.Set("segment", segment)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/aggregations/recommendations?"+params.Encode(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("adaptive-metrics: list recommended rules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("adaptive-metrics: list recommended rules: status %d: %s", resp.StatusCode, serverError(b))
	}

	rulesVersion := resp.Header.Get("Rules-Version")

	var rules []MetricRule
	if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
		return nil, "", fmt.Errorf("adaptive-metrics: list recommended rules: decode: %w", err)
	}

	return rules, rulesVersion, nil
}
