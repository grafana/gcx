package influxdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"k8s.io/client-go/rest"
)

const maxResponseBytes = 50 << 20 // 50 MB

// Client is a client for executing InfluxDB queries via Grafana's datasource API.
type Client struct {
	restConfig config.NamespacedRESTConfig
	httpClient *http.Client
}

// NewClient creates a new InfluxDB query client.
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

// Query executes an InfluxDB query via Grafana's datasource API.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	body, err := c.buildQueryBody(datasourceUID, req)
	if err != nil {
		return nil, err
	}

	respBody, err := c.executeQuery(ctx, body)
	if err != nil {
		return nil, err
	}

	var grafanaResp GrafanaQueryResponse
	if err := json.Unmarshal(respBody, &grafanaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result, ok := grafanaResp.Results["A"]; ok {
		if result.Error != "" {
			return nil, fmt.Errorf("query error: %s", result.Error)
		}
	}

	return convertGrafanaResponse(&grafanaResp), nil
}

// Measurements returns measurement names from the InfluxDB datasource.
func (c *Client) Measurements(ctx context.Context, datasourceUID string, mode Mode, bucket string) (*MeasurementsResponse, error) {
	var queryExpr string

	switch mode {
	case ModeFlux:
		if bucket == "" {
			return nil, errors.New("--bucket is required for Flux mode measurements")
		}
		queryExpr = fmt.Sprintf("import \"influxdata/influxdb/schema\"\nschema.measurements(bucket: %q)", bucket)
	default:
		queryExpr = "SHOW MEASUREMENTS"
	}

	req := QueryRequest{
		Query: queryExpr,
		Mode:  mode,
	}

	body, err := c.buildQueryBody(datasourceUID, req)
	if err != nil {
		return nil, err
	}

	respBody, err := c.executeQuery(ctx, body)
	if err != nil {
		return nil, err
	}

	var grafanaResp GrafanaQueryResponse
	if err := json.Unmarshal(respBody, &grafanaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result, ok := grafanaResp.Results["A"]; ok {
		if result.Error != "" {
			return nil, fmt.Errorf("query error: %s", result.Error)
		}
	}

	return extractMeasurements(&grafanaResp), nil
}

// FieldKeys returns field keys from the InfluxDB datasource. InfluxQL only.
func (c *Client) FieldKeys(ctx context.Context, datasourceUID string, measurement string) (*FieldKeysResponse, error) {
	queryExpr := "SHOW FIELD KEYS"
	if measurement != "" {
		queryExpr = fmt.Sprintf(`SHOW FIELD KEYS FROM %q`, measurement)
	}

	req := QueryRequest{
		Query: queryExpr,
		Mode:  ModeInfluxQL,
	}

	body, err := c.buildQueryBody(datasourceUID, req)
	if err != nil {
		return nil, err
	}

	respBody, err := c.executeQuery(ctx, body)
	if err != nil {
		return nil, err
	}

	var grafanaResp GrafanaQueryResponse
	if err := json.Unmarshal(respBody, &grafanaResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result, ok := grafanaResp.Results["A"]; ok {
		if result.Error != "" {
			return nil, fmt.Errorf("query error: %s", result.Error)
		}
	}

	return extractFieldKeys(&grafanaResp), nil
}

func (c *Client) buildQueryBody(datasourceUID string, req QueryRequest) ([]byte, error) {
	query := map[string]any{
		"refId": "A",
		"datasource": map[string]any{
			"type": "influxdb",
			"uid":  datasourceUID,
		},
		"query": req.Query,
	}

	switch req.Mode {
	case ModeFlux:
		// Flux mode: just the query, no rawQuery or resultFormat.
	default:
		// InfluxQL (and SQL): use rawQuery and resultFormat.
		query["rawQuery"] = true
		query["resultFormat"] = "table"
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

	return body, nil
}

func (c *Client) executeQuery(ctx context.Context, body []byte) ([]byte, error) {
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
		return nil, fmt.Errorf("query failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (c *Client) buildQueryPath() string {
	return fmt.Sprintf("/apis/query.grafana.app/v0alpha1/namespaces/%s/query",
		c.restConfig.Namespace)
}

func convertGrafanaResponse(grafanaResp *GrafanaQueryResponse) *QueryResponse {
	result := &QueryResponse{}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok || len(grafanaResult.Frames) == 0 {
		return result
	}

	// Use the first frame to establish column names and detect time columns.
	firstFrame := grafanaResult.Frames[0]
	numCols := len(firstFrame.Schema.Fields)
	columns := make([]string, numCols)
	timeColumns := make(map[int]bool)
	for i, field := range firstFrame.Schema.Fields {
		columns[i] = field.Name
		if field.Type == "time" {
			timeColumns[i] = true
		}
	}
	result.Columns = columns
	result.TimeColumns = timeColumns

	// Collect rows from all frames. Flux queries return one frame per series,
	// so iterating all frames is required to return complete results.
	for _, frame := range grafanaResult.Frames {
		if len(frame.Data.Values) == 0 || len(frame.Data.Values[0]) == 0 {
			continue
		}

		rowCount := len(frame.Data.Values[0])
		for rowIdx := range rowCount {
			row := make([]any, numCols)
			for colIdx := range numCols {
				if colIdx < len(frame.Data.Values) && rowIdx < len(frame.Data.Values[colIdx]) {
					row[colIdx] = frame.Data.Values[colIdx][rowIdx]
				}
			}
			result.Rows = append(result.Rows, row)
		}
	}

	return result
}

func extractMeasurements(grafanaResp *GrafanaQueryResponse) *MeasurementsResponse {
	result := &MeasurementsResponse{
		Measurements: []string{},
	}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok || len(grafanaResult.Frames) == 0 {
		return result
	}

	frame := grafanaResult.Frames[0]
	if len(frame.Data.Values) == 0 || len(frame.Data.Values[0]) == 0 {
		return result
	}

	for _, v := range frame.Data.Values[0] {
		if s, ok := v.(string); ok {
			result.Measurements = append(result.Measurements, s)
		}
	}

	return result
}

func extractFieldKeys(grafanaResp *GrafanaQueryResponse) *FieldKeysResponse {
	result := &FieldKeysResponse{
		Fields: []FieldKey{},
	}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok || len(grafanaResult.Frames) == 0 {
		return result
	}

	frame := grafanaResult.Frames[0]
	if len(frame.Data.Values) < 2 {
		return result
	}

	rowCount := len(frame.Data.Values[0])
	for i := range rowCount {
		var fieldKey, fieldType string
		if i < len(frame.Data.Values[0]) {
			if s, ok := frame.Data.Values[0][i].(string); ok {
				fieldKey = s
			}
		}
		if i < len(frame.Data.Values[1]) {
			if s, ok := frame.Data.Values[1][i].(string); ok {
				fieldType = s
			}
		}
		result.Fields = append(result.Fields, FieldKey{
			FieldKey:  fieldKey,
			FieldType: fieldType,
		})
	}

	return result
}
