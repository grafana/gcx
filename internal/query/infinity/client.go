package infinity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const maxResponseBytes = 50 << 20 // 50 MB

// Client executes Infinity queries via Grafana's datasource query API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new Infinity query client.
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

// Query executes an Infinity query against the specified datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	apiPath := c.buildQueryPath()

	urlOptions := map[string]any{
		"method":                 req.Method,
		"params":                 []any{},
		"headers":                buildHeaders(req.Headers),
		"data":                   "",
		"body_type":              "",
		"body_content_type":      "",
		"body_graphql_query":     "",
		"body_graphql_variables": "{}",
	}

	if req.Type == "graphql" && req.GraphQL != "" {
		urlOptions["method"] = "POST"
		urlOptions["body_type"] = "graphql"
		urlOptions["body_content_type"] = "application/json"
		urlOptions["body_graphql_query"] = req.GraphQL
	}

	query := map[string]any{
		"refId": "A",
		"datasource": map[string]any{
			"type": "yesoreyeram-infinity-datasource",
			"uid":  datasourceUID,
		},
		"type":             req.Type,
		"source":           req.Source,
		"format":           "table",
		"url":              req.URL,
		"url_options":      urlOptions,
		"parser":           "backend",
		"root_selector":    req.RootSelector,
		"columns":          []any{},
		"filters":          []any{},
		"computed_columns": []any{},
		"data":             req.Data,
		"filterExpression": "",
		"uql":              "",
		"groq":             "",
	}

	var from, to string
	if req.IsRange() {
		from = strconv.FormatInt(req.Start.UnixMilli(), 10)
		to = strconv.FormatInt(req.End.UnixMilli(), 10)
	} else {
		from = "now-1h"
		to = "now"
	}

	bodyMap := map[string]any{
		"queries": []any{query},
		"from":    from,
		"to":      to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	statusCode, respBody, err := c.doQuery(ctx, apiPath, body)
	if err != nil {
		return nil, err
	}

	// Fall back to legacy API path on 404.
	if statusCode == http.StatusNotFound {
		statusCode, respBody, err = c.doQuery(ctx, "/api/ds/query", body)
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		return nil, queryerror.FromBody("infinity", "query", statusCode, respBody)
	}

	var grafanaResp GrafanaQueryResponse
	if err := json.Unmarshal(respBody, &grafanaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result, ok := grafanaResp.Results["A"]; ok {
		if result.Error != "" {
			status := result.Status
			if status == 0 {
				status = http.StatusBadRequest
			}
			return nil, queryerror.New("infinity", "query", status, result.Error, result.ErrorSource)
		}
	}

	return ConvertGrafanaResponse(&grafanaResp), nil
}

// doQuery sends a POST request to the given path and returns the HTTP status code and body.
func (c *Client) doQuery(ctx context.Context, path string, body []byte) (int, []byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+path, bytes.NewBuffer(body))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to read response: %w", err)
	}

	return resp.StatusCode, respBody, nil
}

func (c *Client) buildQueryPath() string {
	return fmt.Sprintf("/apis/query.grafana.app/v0alpha1/namespaces/%s/query",
		c.restConfig.Namespace)
}

func buildHeaders(headers map[string]string) []any {
	if len(headers) == 0 {
		return []any{}
	}
	result := make([]any, 0, len(headers))
	for k, v := range headers {
		result = append(result, map[string]string{"key": k, "value": v})
	}
	return result
}
