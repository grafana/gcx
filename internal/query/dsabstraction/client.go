package dsabstraction

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const (
	maxResponseBytes = 50 << 20 // 50 MB
	apiGroupVersion  = "dsabstraction.grafana.app/v1alpha1"
)

// Client is a client for the dsabstraction SQL query endpoint.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new dsabstraction query client.
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

// Namespace returns the namespace the client targets.
func (c *Client) Namespace() string {
	return c.restConfig.Namespace
}

// Query executes a SQL query against the dsabstraction endpoint.
func (c *Client) Query(ctx context.Context, req SQLRequest) (*SQLResponse, error) {
	if req.SQL == "" {
		return nil, errors.New("sql is required")
	}

	body, err := json.Marshal(requestBody{
		Query:    req.SQL,
		From:     req.From,
		To:       req.To,
		Pushdown: req.Pushdown,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	apiPath := c.buildQueryPath()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if req.Cookie != "" {
		httpReq.Header.Set("Cookie", req.Cookie)
	}

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
		return nil, queryerror.FromBody("dsabstraction", "query", resp.StatusCode, respBody)
	}

	var out SQLResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &out, nil
}

func (c *Client) buildQueryPath() string {
	return fmt.Sprintf("/apis/%s/namespaces/%s/query", apiGroupVersion, c.restConfig.Namespace)
}
