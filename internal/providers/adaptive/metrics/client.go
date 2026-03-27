package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// Client is an HTTP client for the Grafana Adaptive Metrics API.
type Client struct {
	baseURL    string
	tenantID   int
	apiToken   string
	httpClient *http.Client
}

// NewClient creates a new Adaptive Metrics client.
// If httpClient is nil, http.DefaultClient is used.
func NewClient(baseURL string, tenantID int, apiToken string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    baseURL,
		tenantID:   tenantID,
		apiToken:   apiToken,
		httpClient: httpClient,
	}
}

// doRequest builds and executes a request against the Adaptive Metrics API.
func (c *Client) doRequest(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("metrics: marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("metrics: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(strconv.Itoa(c.tenantID), c.apiToken)

	return c.httpClient.Do(req)
}

// ListRules returns all aggregation rules and the current ETag.
func (c *Client) ListRules(ctx context.Context) ([]MetricRule, string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/aggregations/rules", nil)
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

// SyncRules replaces the aggregation rules, using the given ETag for optimistic concurrency.
func (c *Client) SyncRules(ctx context.Context, rules []MetricRule, etag string) error {
	data, err := json.Marshal(rules)
	if err != nil {
		return fmt.Errorf("metrics: sync rules: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/aggregations/rules", bytes.NewReader(data))
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

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("metrics: sync rules: status %d: %s", resp.StatusCode, string(b))
	}

	return nil
}

// ListRecommendations returns all metric aggregation recommendations.
func (c *Client) ListRecommendations(ctx context.Context) ([]MetricRecommendation, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/aggregations/recommendations", nil)
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
