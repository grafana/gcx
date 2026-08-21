package mysql

import (
	"context"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	querysql "github.com/grafana/gcx/internal/query/sql"
)

// pluginID is the Grafana MySQL datasource plugin ID.
const pluginID = "mysql"

// Client is a client for executing MySQL queries via Grafana's datasource API.
type Client struct {
	queryClient *grafanaquery.Client
}

// NewClient creates a new MySQL query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	queryClient, err := grafanaquery.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{queryClient: queryClient}, nil
}

// Query executes a MySQL query against the specified datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*querysql.QueryResponse, error) {
	body, err := querysql.BuildRawQueryBody(pluginID, datasourceUID, querysql.RawQueryRequest{
		RawSQL:     req.RawSQL,
		Start:      req.Start,
		End:        req.End,
		IntervalMs: req.IntervalMs,
	})
	if err != nil {
		return nil, err
	}

	respBody, err := c.queryClient.Execute(ctx, body, "mysql", "query")
	if err != nil {
		return nil, err
	}

	return querysql.ParseResponse(respBody, "mysql")
}
