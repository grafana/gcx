package prometheus

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
	"k8s.io/client-go/rest"
)

const maxResponseBytes = 50 << 20 // 50 MB

// Client is a client for executing Prometheus queries via Grafana's datasource API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new Prometheus query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		restConfig: cfg,
		httpClient: httpClient,
	}, nil
}

// Query executes a Prometheus query against the specified datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	apiPath := c.buildQueryPath()

	// Build the query object with datasource info
	query := map[string]any{
		"refId": "A",
		"datasource": map[string]any{
			"type": "prometheus",
			"uid":  datasourceUID,
		},
		"expr":       req.Query,
		"intervalMs": 60000,
	}

	// Determine time range
	var from, to string
	if req.IsRange() {
		from = strconv.FormatInt(req.Start.UnixMilli(), 10)
		to = strconv.FormatInt(req.End.UnixMilli(), 10)
		if req.Step > 0 {
			query["intervalMs"] = req.Step.Milliseconds()
		}
	} else {
		// For instant queries, use a small time window
		from = "now-1m"
		to = "now"
		query["instant"] = true
	}

	// Build request body
	bodyMap := map[string]any{
		"queries": []any{query},
		"from":    from,
		"to":      to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the Grafana datasource response format
	var grafanaResp GrafanaQueryResponse
	if err := json.Unmarshal(respBody, &grafanaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for errors in the response
	if result, ok := grafanaResp.Results["A"]; ok {
		if result.Error != "" {
			return nil, fmt.Errorf("query error: %s", result.Error)
		}
	}

	// Convert to Prometheus-style response
	return convertGrafanaResponse(&grafanaResp), nil
}

// Labels returns all label names.
func (c *Client) Labels(ctx context.Context, datasourceUID string) (*LabelsResponse, error) {
	apiPath := c.buildLabelsPath(datasourceUID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get labels: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("labels query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result LabelsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// LabelValues returns values for a specific label.
func (c *Client) LabelValues(ctx context.Context, datasourceUID, labelName string) (*LabelsResponse, error) {
	apiPath := c.buildLabelValuesPath(datasourceUID, labelName)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get label values: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("label values query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result LabelsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// Metadata returns metric metadata.
func (c *Client) Metadata(ctx context.Context, datasourceUID string, metric string) (*MetadataResponse, error) {
	apiPath := c.buildMetadataPath(datasourceUID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if metric != "" {
		q := httpReq.URL.Query()
		q.Set("metric", metric)
		httpReq.URL.RawQuery = q.Encode()
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get metadata: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result MetadataResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

func (c *Client) buildQueryPath() string {
	return fmt.Sprintf("/apis/query.grafana.app/v0alpha1/namespaces/%s/query",
		c.restConfig.Namespace)
}

func (c *Client) buildLabelsPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/labels", url.PathEscape(datasourceUID))
}

func (c *Client) buildLabelValuesPath(datasourceUID, labelName string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/label/%s/values",
		url.PathEscape(datasourceUID), url.PathEscape(labelName))
}

func (c *Client) buildMetadataPath(datasourceUID string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/api/v1/metadata", url.PathEscape(datasourceUID))
}
