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

	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/resources/adapter"
)

// Sentinel errors for specific HTTP status codes.
var (
	ErrRuleNotFound       = fmt.Errorf("rule: %w", adapter.ErrNotFound)
	ErrPreconditionFailed = errors.New("rule was modified concurrently")
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

// doRequest builds and executes a GET request against the Adaptive Metrics API.
func (c *Client) doRequest(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("metrics: create request: %w", err)
	}
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	return c.httpClient.Do(req)
}

// ListRules returns all aggregation rules and the current ETag.
func (c *Client) ListRules(ctx context.Context, segment string) ([]MetricRule, string, error) {
	path := "/aggregations/rules"
	if segment != "" {
		path += "?segment=" + url.QueryEscape(segment)
	}

	resp, err := c.doRequest(ctx, path)
	if err != nil {
		return nil, "", fmt.Errorf("metrics: list rules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("metrics: list rules: status %d: %s", resp.StatusCode, string(b))
	}

	etag := resp.Header.Get("Etag")

	var rules []MetricRule
	if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
		return nil, "", fmt.Errorf("metrics: list rules: decode: %w", err)
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

	resp, err := c.doRequest(ctx, path)
	if err != nil {
		return MetricRule{}, fmt.Errorf("metrics: get rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return MetricRule{}, ErrRuleNotFound
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return MetricRule{}, fmt.Errorf("metrics: get rule: status %d: %s", resp.StatusCode, string(b))
	}

	var rule MetricRule
	if err := json.NewDecoder(resp.Body).Decode(&rule); err != nil {
		return MetricRule{}, fmt.Errorf("metrics: get rule: decode: %w", err)
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
		return "", fmt.Errorf("metrics: create rule: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("metrics: create rule: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("metrics: create rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return "", ErrPreconditionFailed
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("metrics: create rule: status %d: %s", resp.StatusCode, string(b))
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
		return "", fmt.Errorf("metrics: update rule: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("metrics: update rule: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("metrics: update rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return "", ErrPreconditionFailed
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("metrics: update rule: status %d: %s", resp.StatusCode, string(b))
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
		return fmt.Errorf("metrics: delete rule: create request: %w", err)
	}
	req.Header.Set("If-Match", etag)
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("metrics: delete rule: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return ErrPreconditionFailed
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("metrics: delete rule: status %d: %s", resp.StatusCode, string(b))
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
		return fmt.Errorf("metrics: sync rules: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("metrics: sync rules: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("metrics: sync rules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPreconditionFailed {
		return ErrPreconditionFailed
	}
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("metrics: sync rules: status %d: %s", resp.StatusCode, string(b))
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
		return nil, fmt.Errorf("metrics: validate rules: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("metrics: validate rules: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics: validate rules: %w", err)
	}
	defer resp.Body.Close()

	// 200 = valid rules; 400 = validation errors as []string — both are decoded the same way.
	// Other non-2xx/400 statuses are genuine errors.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("metrics: validate rules: status %d: %s", resp.StatusCode, string(b))
	}

	var errs []string
	if err := json.NewDecoder(resp.Body).Decode(&errs); err != nil {
		return nil, fmt.Errorf("metrics: validate rules: decode: %w", err)
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

	resp, err := c.doRequest(ctx, "/aggregations/recommendations?"+params.Encode())
	if err != nil {
		return nil, fmt.Errorf("metrics: list recommendations: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("metrics: list recommendations: status %d: %s", resp.StatusCode, string(b))
	}

	var recs []MetricRecommendation
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		return nil, fmt.Errorf("metrics: list recommendations: decode: %w", err)
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

	resp, err := c.doRequest(ctx, "/aggregations/recommendations?"+params.Encode())
	if err != nil {
		return nil, "", fmt.Errorf("metrics: list recommended rules: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("metrics: list recommended rules: status %d: %s", resp.StatusCode, string(b))
	}

	rulesVersion := resp.Header.Get("Rules-Version")

	var rules []MetricRule
	if err := json.NewDecoder(resp.Body).Decode(&rules); err != nil {
		return nil, "", fmt.Errorf("metrics: list recommended rules: decode: %w", err)
	}

	return rules, rulesVersion, nil
}
