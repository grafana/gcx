package mssql

import (
	"context"

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
	body, err := querysql.BuildRawQueryBody(DatasourceType, datasourceUID, querysql.RawQueryRequest{
		RawSQL:     req.RawSQL,
		Start:      req.Start,
		End:        req.End,
		IntervalMs: req.IntervalMs,
	})
	if err != nil {
		return nil, err
	}

	respBody, err := c.queryClient.Execute(ctx, body, "mssql", "query")
	if err != nil {
		return nil, err
	}

	return querysql.ParseResponse(respBody, "mssql")
}
