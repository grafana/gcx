package schemads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const (
	maxResponseBytes = 50 << 20 // 50 MB
	basePath         = "abstractionSchema"
	fullSchemaPath   = "fullSchema"
	columnsPath      = "columns"
)

// Client fetches schema information from a Grafana datasource that
// implements the schemads protocol.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient constructs a schemads client.
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

// FullSchema returns the complete schema for the datasource at the given UID.
// Returns a typed *queryerror.APIError on non-2xx.
func (c *Client) FullSchema(ctx context.Context, datasourceUID string) (*Schema, error) {
	apiPath := c.buildPath(datasourceUID, fullSchemaPath)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch schema: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("schemads", "fullSchema", resp.StatusCode, body)
	}

	var out FullSchemaResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("schema error: %s", out.Error)
	}
	if out.FullSchema == nil {
		return &Schema{}, nil
	}
	return out.FullSchema, nil
}

// Columns returns the dynamic per-table columns for the given tables, along
// with any table-level metadata the datasource opportunistically attaches
// (Prometheus HELP/TYPE/UNIT lives here today). Unlike FullSchema, this
// includes columns that the datasource only knows after binding to a
// specific table (e.g. Prometheus label dimensions).
func (c *Client) Columns(ctx context.Context, datasourceUID string, tables []string, tableParameters map[string]string) (*ColumnsResponse, error) {
	if len(tables) == 0 {
		return &ColumnsResponse{Columns: map[string][]Column{}}, nil
	}
	apiPath := c.buildPath(datasourceUID, columnsPath)

	body, err := json.Marshal(struct {
		Tables          []string          `json:"tables"`
		TableParameters map[string]string `json:"tableParameters,omitempty"`
	}{Tables: tables, TableParameters: tableParameters})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch columns: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("schemads", "columns", resp.StatusCode, respBody)
	}

	var out ColumnsResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if out.Columns == nil {
		out.Columns = map[string][]Column{}
	}
	return &out, nil
}

func (c *Client) buildPath(datasourceUID, requestType string) string {
	return fmt.Sprintf("/api/datasources/uid/%s/resources/%s/%s",
		url.PathEscape(datasourceUID), basePath, requestType)
}
