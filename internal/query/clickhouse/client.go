package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/queryerror"
)

// Client is a client for executing ClickHouse queries via Grafana's datasource API.
type Client struct {
	queryClient *grafanaquery.Client
}

// NewClient creates a new ClickHouse query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	queryClient, err := grafanaquery.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{queryClient: queryClient}, nil
}

// Query executes a ClickHouse query against the specified datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
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
				"refId":      "A",
				"datasource": map[string]any{"type": "grafana-clickhouse-datasource", "uid": datasourceUID},
				"rawSql":     req.RawSQL,
				"format":     1,
				"intervalMs": intervalMs,
			},
		},
		"from": from,
		"to":   to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, err := c.queryClient.Execute(ctx, body, "clickhouse", "query")
	if err != nil {
		return nil, err
	}

	return parseResponse(respBody)
}

func parseResponse(respBody []byte) (*QueryResponse, error) {
	var raw GrafanaQueryResponse
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result, ok := raw.Results["A"]
	if !ok {
		return &QueryResponse{}, nil
	}
	if result.Error != "" {
		status := result.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		return nil, queryerror.New("clickhouse", "query", status, result.Error, result.ErrorSource)
	}

	resp := &QueryResponse{}

	// ClickHouse with FormatOptionTable (format=1) produces exactly one frame per
	// result set. Only the first frame is used.
	// See: github.com/grafana/clickhouse-datasource/blob/c56e3af64308ffc4cf304c8f4dd9ce545d6b2063/pkg/plugin/driver.go#L448-L453
	// Ref: github.com/grafana/mcp-grafana/blob/9517bc0fc86b4d4f8a5978f28498bf376dc51d65/tools/clickhouse.go#L326-L349
	if len(result.Frames) == 0 {
		return resp, nil
	}
	frame := result.Frames[0]

	for _, f := range frame.Schema.Fields {
		resp.Columns = append(resp.Columns, Column{Name: f.Name, Type: f.Type})
	}

	if len(frame.Data.Values) == 0 || len(frame.Data.Values[0]) == 0 {
		return resp, nil
	}
	numRows := len(frame.Data.Values[0])
	for rowIdx := range numRows {
		row := make([]any, len(frame.Data.Values))
		for colIdx := range frame.Data.Values {
			if rowIdx < len(frame.Data.Values[colIdx]) {
				row[colIdx] = frame.Data.Values[colIdx][rowIdx]
			}
		}
		resp.Rows = append(resp.Rows, row)
	}
	return resp, nil
}
