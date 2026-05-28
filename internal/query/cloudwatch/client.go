package cloudwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const (
	maxQueryResponseBytes    = 50 << 20 // 50 MB
	maxResourceResponseBytes = 1 << 20  // 1 MB
)

// Client is an HTTP client for CloudWatch queries and resource listing via Grafana's proxy.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new CloudWatch query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{restConfig: cfg, httpClient: httpClient}, nil
}

// Query executes a CloudWatch metric query via the Grafana datasource proxy.
func (c *Client) Query(ctx context.Context, dsUID string, req QueryRequest) (*QueryResponse, error) {
	query := map[string]any{
		"refId": "A",
		"datasource": map[string]any{
			"type": "cloudwatch",
			"uid":  dsUID,
		},
		"queryType":  "timeSeriesQuery",
		"namespace":  req.Namespace,
		"metricName": req.MetricName,
		"region":     req.Region,
		"statistic":  req.Statistic,
		"matchExact": req.MatchExact,
		// Period must be sent as a string. The Grafana CloudWatch plugin
		// rejects numeric values with a 500. The wire-shape inconsistency
		// with intervalMs (numeric, set below) reflects the upstream
		// contract, not a gcx choice; do not "fix" without re-verifying
		// against the plugin.
		"period":          req.Period,
		"dimensions":      orEmptyDimensions(req.Dimensions),
		"expression":      "",
		"metricQueryType": 0,
	}
	if req.AccountID != "" {
		query["accountId"] = req.AccountID
	}

	intervalMs := req.IntervalMs
	if intervalMs == 0 {
		// Period may be "auto"; only derive intervalMs from a numeric period.
		// Otherwise default to 60s so the field stays a JSON number — the
		// plugin re-derives the actual step from the time range anyway.
		if p, err := strconv.Atoi(req.Period); err == nil && p > 0 {
			intervalMs = int64(p) * 1000
		} else {
			intervalMs = 60_000
		}
	}
	query["intervalMs"] = intervalMs

	bodyMap := map[string]any{
		"queries": []any{query},
		"from":    strconv.FormatInt(req.Start.UnixMilli(), 10),
		"to":      strconv.FormatInt(req.End.UnixMilli(), 10),
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	apiPath := c.buildK8sQueryPath()
	respBody, statusCode, err := c.post(ctx, apiPath, body)
	if err != nil {
		return nil, err
	}

	// Fall back to legacy /api/ds/query if K8s query API is not available.
	if statusCode == http.StatusNotFound {
		apiPath = "/api/ds/query"
		respBody, statusCode, err = c.post(ctx, apiPath, body)
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		return nil, queryerror.FromBody("cloudwatch", "query", statusCode, respBody)
	}

	return ParseQueryResponse(respBody)
}

// ListNamespaces returns the available CloudWatch namespaces.
func (c *Client) ListNamespaces(ctx context.Context, dsUID, region, accountID string) ([]string, error) {
	params := url.Values{}
	if region != "" {
		params.Set("region", region)
	}
	if accountID != "" {
		params.Set("accountId", accountID)
	}

	body, err := c.getResource(ctx, dsUID, "namespaces", params)
	if err != nil {
		return nil, err
	}
	return ParseNamespaces(body)
}

// ListMetrics returns the CloudWatch metrics in a given namespace.
func (c *Client) ListMetrics(ctx context.Context, dsUID, region, namespace, accountID string) ([]Metric, error) {
	params := url.Values{}
	params.Set("region", region)
	params.Set("namespace", namespace)
	if accountID != "" {
		params.Set("accountId", accountID)
	}

	body, err := c.getResource(ctx, dsUID, "metrics", params)
	if err != nil {
		return nil, err
	}
	return ParseMetrics(body)
}

// ListDimensionKeys returns the available dimension keys for a metric.
func (c *Client) ListDimensionKeys(ctx context.Context, dsUID, region, namespace, metric, accountID string) ([]string, error) {
	params := url.Values{}
	params.Set("region", region)
	params.Set("namespace", namespace)
	params.Set("metricName", metric)
	if accountID != "" {
		params.Set("accountId", accountID)
	}

	body, err := c.getResource(ctx, dsUID, "dimension-keys", params)
	if err != nil {
		return nil, err
	}
	return ParseDimensionKeys(body)
}

// ListRegions returns all available AWS regions for the datasource.
func (c *Client) ListRegions(ctx context.Context, dsUID string) ([]string, error) {
	body, err := c.getResource(ctx, dsUID, "regions", nil)
	if err != nil {
		return nil, err
	}
	return ParseRegions(body)
}

// ListAccounts returns the AWS accounts accessible via this datasource.
func (c *Client) ListAccounts(ctx context.Context, dsUID, region string) ([]Account, error) {
	params := url.Values{}
	if region != "" {
		params.Set("region", region)
	}

	body, err := c.getResource(ctx, dsUID, "accounts", params)
	if err != nil {
		return nil, err
	}
	return ParseAccounts(body)
}

func (c *Client) getResource(ctx context.Context, dsUID, resource string, params url.Values) ([]byte, error) {
	path := fmt.Sprintf("/api/datasources/uid/%s/resources/%s", url.PathEscape(dsUID), resource)
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResourceResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("cloudwatch", resource, resp.StatusCode, body)
	}

	return body, nil
}

func (c *Client) post(ctx context.Context, apiPath string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBuffer(body))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxQueryResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

func (c *Client) buildK8sQueryPath() string {
	return fmt.Sprintf("/apis/query.grafana.app/v0alpha1/namespaces/%s/query",
		c.restConfig.Namespace)
}

// orEmptyDimensions returns an empty map (instead of nil) so the JSON body
// always carries a `dimensions: {}` field rather than `dimensions: null`.
// Some Grafana CloudWatch plugin versions are stricter about absent fields
// vs explicit empties.
func orEmptyDimensions(dims map[string][]string) map[string][]string {
	if dims == nil {
		return map[string][]string{}
	}
	return dims
}
