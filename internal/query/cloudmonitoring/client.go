package cloudmonitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const (
	maxResourceResponseBytes = 8 << 20 // unfiltered descriptor listings can be large

	// pluginID is the Grafana Google Cloud Monitoring datasource plugin ID
	// (the pre-rebrand "stackdriver" name is baked into the plugin).
	pluginID = "stackdriver"
)

// Client executes Google Cloud Monitoring queries and discovery calls via
// Grafana's datasource APIs.
type Client struct {
	restConfig  config.NamespacedRESTConfig
	httpClient  *http.Client
	queryClient *grafanaquery.Client
}

// NewClient creates a new Google Cloud Monitoring query client.
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

// Query executes a time-series list query via the Grafana datasource query API.
func (c *Client) Query(ctx context.Context, dsUID string, req QueryRequest) (*QueryResponse, error) {
	filters := make([]string, 0, 3+4*len(req.Filters))
	filters = append(filters, "metric.type", "=", req.MetricType)
	for key, value := range req.Filters {
		filters = append(filters, "AND", key, "=", value)
	}

	groupBys := req.GroupBys
	if groupBys == nil {
		groupBys = []string{}
	}

	alignmentPeriod := req.AlignmentPeriod
	if alignmentPeriod == "" {
		alignmentPeriod = "cloud-monitoring-auto"
	}

	query := map[string]any{
		"refId":     "A",
		"queryType": "timeSeriesList",
		"datasource": map[string]any{
			"type": pluginID,
			"uid":  dsUID,
		},
		"timeSeriesList": map[string]any{
			"projectName":        req.Project,
			"filters":            filters,
			"crossSeriesReducer": req.Reducer,
			"perSeriesAligner":   req.Aligner,
			"alignmentPeriod":    alignmentPeriod,
			"groupBys":           groupBys,
		},
		"intervalMs":    60_000,
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

	respBody, err := c.queryClient.Execute(ctx, body, "cloudmonitoring", "query")
	if err != nil {
		return nil, simplifyQueryError(err)
	}

	return ParseQueryResponse(respBody)
}

// simplifyQueryError rewrites the GCP error envelope carried inside a query
// API error message to its status and message.
func simplifyQueryError(err error) error {
	var apiErr *queryerror.APIError
	if errors.As(err, &apiErr) {
		apiErr.Message = simplifyGCPError(apiErr.Message)
	}
	return err
}

// ListProjects returns the GCP projects visible to the datasource.
func (c *Client) ListProjects(ctx context.Context, dsUID string) ([]Project, error) {
	body, err := c.getResource(ctx, dsUID, "projects", "projects")
	if err != nil {
		return nil, err
	}
	return ParseProjects(body)
}

// ListMetricDescriptors returns metric descriptors for a project. servicePrefix
// (e.g. "compute.googleapis.com") narrows the listing server-side; without it
// the plugin pages through every descriptor in the project, which can be slow.
func (c *Client) ListMetricDescriptors(ctx context.Context, dsUID, project, servicePrefix string) ([]MetricDescriptor, error) {
	path := fmt.Sprintf("metricDescriptors/v3/projects/%s/metricDescriptors", url.PathEscape(project))
	if servicePrefix != "" {
		filter := fmt.Sprintf(`metric.type = starts_with(%q)`, servicePrefix)
		path += "?filter=" + url.QueryEscape(filter)
	}

	body, err := c.getResource(ctx, dsUID, path, "metric-descriptors")
	if err != nil {
		return nil, err
	}
	return ParseMetricDescriptors(body)
}

func (c *Client) getResource(ctx context.Context, dsUID, pathAndQuery, operation string) ([]byte, error) {
	path := fmt.Sprintf("/api/datasources/uid/%s/resources/%s", url.PathEscape(dsUID), pathAndQuery)

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
		return nil, queryerror.FromBody("cloudmonitoring", operation, resp.StatusCode, body)
	}

	return body, nil
}
