package infinity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/queryerror"
)

// Client executes Infinity queries via Grafana's datasource query API.
type Client struct {
	queryClient *grafanaquery.Client
}

// NewClient creates a new Infinity query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	queryClient, err := grafanaquery.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{queryClient: queryClient}, nil
}

// Query executes an Infinity query against the specified datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	query := map[string]any{
		"refId": "A",
		"datasource": map[string]any{
			"type": "yesoreyeram-infinity-datasource",
			"uid":  datasourceUID,
		},
		"source":           "url",
		"format":           "table",
		"url":              "",
		"parser":           "backend",
		"root_selector":    req.Expr,
		"columns":          []any{},
		"filters":          []any{},
		"computed_columns": []any{},
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

	respBody, err := c.queryClient.Execute(ctx, body, "infinity", "query")
	if err != nil {
		return nil, err
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
