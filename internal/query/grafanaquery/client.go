package grafanaquery

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

// Client executes Grafana datasource query API requests.
type Client struct {
	host             string
	namespace        string
	httpClient       *http.Client
	maxResponseBytes int64
}

// NewClient creates a datasource query API client using the REST config HTTP transport.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return NewClientWithHTTPClient(cfg, httpClient), nil
}

// NewClientWithHTTPClient creates a datasource query API client using an existing HTTP client.
func NewClientWithHTTPClient(cfg config.NamespacedRESTConfig, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	return &Client{
		host:             cfg.Host,
		namespace:        cfg.Namespace,
		httpClient:       httpClient,
		maxResponseBytes: httputils.DefaultResponseLimit,
	}
}

// Execute posts body to Grafana's K8s datasource query API, falls back to the
// legacy /api/ds/query endpoint on 404, and converts non-200 responses to a
// typed query API error for datasource and operation.
func (c *Client) Execute(ctx context.Context, body []byte, datasource, operation string) ([]byte, error) {
	statusCode, respBody, err := c.post(ctx, c.k8sQueryPath(), body)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		statusCode, respBody, err = c.post(ctx, "/api/ds/query", body)
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		return nil, queryerror.FromBody(datasource, operation, statusCode, respBody)
	}

	return respBody, nil
}

func (c *Client) post(ctx context.Context, path string, body []byte) (int, []byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.host+path, bytes.NewBuffer(body))
	if err != nil {
		return 0, nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, c.maxResponseBytes)
	if err != nil {
		return resp.StatusCode, nil, err
	}

	return resp.StatusCode, respBody, nil
}

func (c *Client) k8sQueryPath() string {
	return fmt.Sprintf("/apis/query.grafana.app/v0alpha1/namespaces/%s/query", c.namespace)
}
