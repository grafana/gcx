package mssql

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	querysql "github.com/grafana/gcx/internal/query/sql"
)

// Client executes MSSQL queries via Grafana's unified datasource query API.
type Client struct {
	queryClient *grafanaquery.Client
}

// NewClient creates a new MSSQL query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	queryClient, err := grafanaquery.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{queryClient: queryClient}, nil
}

// Query executes a T-SQL query against the specified MSSQL datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*querysql.QueryResponse, error) {
	from := strconv.FormatInt(req.Start.UnixMilli(), 10)
	to := strconv.FormatInt(req.End.UnixMilli(), 10)
	if req.Start.IsZero() || req.End.IsZero() {
		now := time.Now()
		from = strconv.FormatInt(now.Add(-1*time.Hour).UnixMilli(), 10)
		to = strconv.FormatInt(now.UnixMilli(), 10)
	}

	bodyMap := map[string]any{
		"queries": []any{
			map[string]any{
				"refId":      "A",
				"datasource": map[string]any{"type": DatasourceType, "uid": datasourceUID},
				"rawSql":     req.RawSQL,
				"format":     QueryFormatTable,
			},
		},
		"from": from,
		"to":   to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, err := c.queryClient.Execute(ctx, body, "mssql", "query")
	if err != nil {
		return nil, err
	}

	return querysql.ParseResponse(respBody, "mssql")
}
