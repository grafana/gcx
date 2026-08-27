package pinot

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

// Client is a client for executing PinotQL queries via Grafana's datasource API.
type Client struct {
	queryClient *grafanaquery.Client
}

// NewClient creates a new Pinot query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	queryClient, err := grafanaquery.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{queryClient: queryClient}, nil
}

// Query executes a PinotQL query against the specified StarTree datasource.
// The request body is custom (not querysql.BuildRawQueryBody): StarTree expects
// queryType/editorMode/displayType/tableName/pinotQlCode rather than rawSql.
// tableName is copied from the first FROM clause when present so Explore can
// open; Pinot executes pinotQlCode, and a bad query is returned as a plugin error.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*querysql.QueryResponse, error) {
	intervalMs := req.IntervalMs
	if intervalMs == 0 {
		intervalMs = 60000
	}

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
				"refId":       "A",
				"datasource":  map[string]any{"type": DatasourceType, "uid": datasourceUID},
				"queryType":   "PinotQL",
				"editorMode":  "Code",
				"displayType": "TABLE",
				"tableName":   ExtractTableName(req.RawSQL),
				"pinotQlCode": req.RawSQL,
				"intervalMs":  intervalMs,
			},
		},
		"from": from,
		"to":   to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, err := c.queryClient.Execute(ctx, body, "pinot", "query")
	if err != nil {
		return nil, err
	}

	return querysql.ParseResponse(respBody, "pinot")
}
