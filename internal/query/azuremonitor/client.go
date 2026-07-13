package azuremonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const (
	maxResourceResponseBytes = 4 << 20 // 4 MB — ARM pages can carry up to 1000 resources
	maxResourcePages         = 32      // pagination guard against runaway nextLink chains

	// pluginID is the Grafana Azure Monitor datasource plugin ID.
	pluginID = "grafana-azure-monitor-datasource"
)

// Client executes Azure Monitor metric queries and Azure Resource Manager
// discovery calls via Grafana's datasource APIs.
type Client struct {
	restConfig  config.NamespacedRESTConfig
	httpClient  *http.Client
	queryClient *grafanaquery.Client
}

// NewClient creates a new Azure Monitor query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{
		restConfig:  cfg,
		httpClient:  httpClient,
		queryClient: grafanaquery.NewClientWithHTTPClient(cfg, httpClient),
	}, nil
}

// Query executes an Azure Monitor metrics query via the Grafana datasource query API.
func (c *Client) Query(ctx context.Context, dsUID string, req QueryRequest) (*QueryResponse, error) {
	filters := make([]any, 0, len(req.DimensionFilters))
	for dim, val := range req.DimensionFilters {
		filters = append(filters, map[string]any{
			"dimension": dim,
			"operator":  "eq",
			"filters":   []string{val},
		})
	}

	resource := map[string]any{
		"resourceGroup":   req.ResourceGroup,
		"resourceName":    req.ResourceName,
		"metricNamespace": req.MetricNamespace,
	}
	if req.Region != "" {
		resource["region"] = req.Region
	}

	azm := map[string]any{
		"resources":        []any{resource},
		"metricNamespace":  req.MetricNamespace,
		"metricName":       req.MetricName,
		"aggregation":      req.Aggregation,
		"timeGrain":        req.TimeGrain,
		"dimensionFilters": filters,
	}
	if req.Region != "" {
		azm["region"] = req.Region
	}
	if req.Top != "" {
		azm["top"] = req.Top
	}

	query := map[string]any{
		"refId":     "A",
		"queryType": "Azure Monitor",
		// The plugin backend builds the ARM URL from the query-level
		// subscription field, not from resources[].subscription. Moving this
		// into the resource entry produces a malformed ARM request
		// (InvalidSubscriptionId).
		"subscription": req.Subscription,
		"datasource": map[string]any{
			"type": pluginID,
			"uid":  dsUID,
		},
		"azureMonitor":  azm,
		"intervalMs":    intervalMsFor(req),
		"maxDataPoints": 1000,
	}

	bodyMap := map[string]any{
		"queries": []any{query},
		"from":    strconv.FormatInt(req.Start.UnixMilli(), 10),
		"to":      strconv.FormatInt(req.End.UnixMilli(), 10),
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, err := c.queryClient.Execute(ctx, body, "azuremonitor", "query")
	if err != nil {
		return nil, err
	}

	return ParseQueryResponse(respBody)
}

// intervalMsFor derives the query interval from the time range so the
// plugin's "auto" time grain picks a grain that keeps the series under the
// Azure Monitor API's data-point cap, with a 60s floor (the smallest grain).
func intervalMsFor(req QueryRequest) int64 {
	const minIntervalMs = 60_000
	rangeMs := req.End.Sub(req.Start).Milliseconds()
	if rangeMs <= 0 {
		return minIntervalMs
	}
	interval := rangeMs / 1000
	if interval < minIntervalMs {
		return minIntervalMs
	}
	return interval
}

// ListSubscriptions returns the Azure subscriptions visible to the datasource.
func (c *Client) ListSubscriptions(ctx context.Context, dsUID string) ([]Subscription, error) {
	items, err := c.listARM(ctx, dsUID, "subscriptions", "2020-01-01", "subscriptions")
	if err != nil {
		return nil, err
	}
	return ParseSubscriptions(items)
}

// ListResourceGroups returns the resource groups in a subscription.
func (c *Client) ListResourceGroups(ctx context.Context, dsUID, subscription string) ([]ResourceGroup, error) {
	path := fmt.Sprintf("subscriptions/%s/resourceGroups", url.PathEscape(subscription))
	items, err := c.listARM(ctx, dsUID, path, "2020-01-01", "resource-groups")
	if err != nil {
		return nil, err
	}
	return ParseResourceGroups(items)
}

// ListResources returns the resources in a subscription, optionally scoped to
// a resource group.
func (c *Client) ListResources(ctx context.Context, dsUID, subscription, resourceGroup string) ([]Resource, error) {
	path := "subscriptions/" + url.PathEscape(subscription)
	if resourceGroup != "" {
		path += "/resourceGroups/" + url.PathEscape(resourceGroup)
	}
	path += "/resources"
	items, err := c.listARM(ctx, dsUID, path, "2021-04-01", "resources")
	if err != nil {
		return nil, err
	}
	return ParseResources(items)
}

// ListMetricDefinitions returns the metric definitions available for a resource.
func (c *Client) ListMetricDefinitions(ctx context.Context, dsUID, subscription, resourceGroup, metricNamespace, resourceName string) ([]MetricDefinition, error) {
	path := fmt.Sprintf("subscriptions/%s/resourceGroups/%s/providers/%s/%s/providers/microsoft.insights/metricdefinitions",
		url.PathEscape(subscription),
		url.PathEscape(resourceGroup),
		escapeNamespace(metricNamespace),
		url.PathEscape(resourceName),
	)
	items, err := c.listARM(ctx, dsUID, path, "2018-01-01", "metric-definitions")
	if err != nil {
		return nil, err
	}
	return ParseMetricDefinitions(items)
}

// escapeNamespace path-escapes each segment of a metric namespace such as
// "Microsoft.Storage/storageAccounts" while preserving the segment separators.
func escapeNamespace(ns string) string {
	parts := strings.Split(ns, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// listARM fetches all pages of an Azure Resource Manager list endpoint through
// the datasource resource proxy, following nextLink pagination.
func (c *Client) listARM(ctx context.Context, dsUID, armPath, apiVersion, operation string) ([]json.RawMessage, error) {
	pathAndQuery := armPath + "?api-version=" + url.QueryEscape(apiVersion)

	var items []json.RawMessage
	for range maxResourcePages {
		body, err := c.getResource(ctx, dsUID, pathAndQuery, operation)
		if err != nil {
			return nil, err
		}

		var page armList
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("failed to parse %s response: %w", operation, err)
		}
		items = append(items, page.Value...)

		if page.NextLink == "" {
			return items, nil
		}
		// nextLink is an absolute ARM URL (https://management.azure.com/...);
		// re-route its path and query through the datasource proxy.
		next, err := url.Parse(page.NextLink)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s nextLink: %w", operation, err)
		}
		pathAndQuery = strings.TrimPrefix(next.Path, "/") + "?" + next.RawQuery
	}
	return items, nil
}

// getResource calls the Azure Monitor plugin's resource proxy, which passes
// the request through to Azure Resource Manager.
func (c *Client) getResource(ctx context.Context, dsUID, pathAndQuery, operation string) ([]byte, error) {
	path := fmt.Sprintf("/api/datasources/uid/%s/resources/azuremonitor/%s", url.PathEscape(dsUID), pathAndQuery)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResourceResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("azuremonitor", operation, resp.StatusCode, body)
	}

	return body, nil
}
