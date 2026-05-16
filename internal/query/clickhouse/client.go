package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const maxResponseBytes = 50 << 20 // 50 MB

// Client is a client for executing ClickHouse queries via Grafana's datasource API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new ClickHouse query client.
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

	apiPath := c.buildQueryPath()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Fall back to legacy /api/ds/query if K8s query API doesn't exist.
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		apiPath = "/api/ds/query"
		httpReq, err = http.NewRequestWithContext(ctx, http.MethodPost, c.restConfig.Host+apiPath, bytes.NewBuffer(body))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		resp, err = c.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("failed to execute query: %w", err)
		}
		defer resp.Body.Close()
		respBody, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("clickhouse", "query", resp.StatusCode, respBody)
	}

	return parseResponse(respBody)
}

func parseResponse(respBody []byte) (*QueryResponse, error) {
	var raw grafanaQueryResponse
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
	for i, frame := range result.Frames {
		if i == 0 {
			for _, f := range frame.Schema.Fields {
				resp.Columns = append(resp.Columns, Column(f))
			}
		} else {
			if len(frame.Schema.Fields) != len(resp.Columns) {
				continue
			}
			mismatch := false
			for j, f := range frame.Schema.Fields {
				if f.Name != resp.Columns[j].Name || f.Type != resp.Columns[j].Type {
					mismatch = true
					break
				}
			}
			if mismatch {
				continue
			}
		}

		if len(frame.Data.Values) == 0 || len(frame.Data.Values[0]) == 0 {
			continue
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
	}
	return resp, nil
}

func (c *Client) buildQueryPath() string {
	return fmt.Sprintf("/apis/query.grafana.app/v0alpha1/namespaces/%s/query",
		c.restConfig.Namespace)
}
