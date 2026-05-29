package influxdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"sort"
	"strconv"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/queryerror"
)

// Client is a client for executing InfluxDB queries via Grafana's datasource API.
type Client struct {
	queryClient *grafanaquery.Client
}

// NewClient creates a new InfluxDB query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	queryClient, err := grafanaquery.NewClient(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{queryClient: queryClient}, nil
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
			status := result.Status
			if status == 0 {
				status = http.StatusBadRequest
			}
			return nil, queryerror.New("influxdb", "query", status, result.Error, result.ErrorSource)
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
	case ModeInfluxQL:
		queryExpr = "SHOW MEASUREMENTS"
	default:
		return nil, fmt.Errorf("measurements listing is not supported for %s mode", mode)
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
			status := result.Status
			if status == 0 {
				status = http.StatusBadRequest
			}
			return nil, queryerror.New("influxdb", "measurements query", status, result.Error, result.ErrorSource)
		}
	}

	return extractMeasurements(&grafanaResp), nil
}

// FieldKeys returns field keys from the InfluxDB datasource. InfluxQL only.
func (c *Client) FieldKeys(ctx context.Context, datasourceUID string, measurement string) (*FieldKeysResponse, error) { //nolint:dupl
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
			status := result.Status
			if status == 0 {
				status = http.StatusBadRequest
			}
			return nil, queryerror.New("influxdb", "field keys query", status, result.Error, result.ErrorSource)
		}
	}

	return extractFieldKeys(&grafanaResp), nil
}

// TagKeys returns tag keys from the InfluxDB datasource. InfluxQL only.
func (c *Client) TagKeys(ctx context.Context, datasourceUID string, measurement string) (*TagKeysResponse, error) { //nolint:dupl
	queryExpr := "SHOW TAG KEYS"
	if measurement != "" {
		queryExpr = fmt.Sprintf(`SHOW TAG KEYS FROM %q`, measurement)
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
			status := result.Status
			if status == 0 {
				status = http.StatusBadRequest
			}
			return nil, queryerror.New("influxdb", "tag keys query", status, result.Error, result.ErrorSource)
		}
	}

	return extractTagKeys(&grafanaResp), nil
}

// TagValues returns tag values for a given key from the InfluxDB datasource. InfluxQL only.
func (c *Client) TagValues(ctx context.Context, datasourceUID string, key string, measurement string) (*TagValuesResponse, error) {
	queryExpr := fmt.Sprintf(`SHOW TAG VALUES WITH KEY = %q`, key)
	if measurement != "" {
		queryExpr = fmt.Sprintf(`SHOW TAG VALUES FROM %q WITH KEY = %q`, measurement, key)
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
			status := result.Status
			if status == 0 {
				status = http.StatusBadRequest
			}
			return nil, queryerror.New("influxdb", "tag values query", status, result.Error, result.ErrorSource)
		}
	}

	return extractTagValues(&grafanaResp), nil
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
	return c.queryClient.Execute(ctx, body, "influxdb", "query")
}

func extractTagKeys(grafanaResp *GrafanaQueryResponse) *TagKeysResponse {
	result := &TagKeysResponse{
		TagKeys: []string{},
	}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok || len(grafanaResult.Frames) == 0 {
		return result
	}

	// SHOW TAG KEYS returns one frame per measurement; deduplicate across frames.
	seen := make(map[string]bool)
	for _, frame := range grafanaResult.Frames {
		if len(frame.Data.Values) == 0 || len(frame.Data.Values[0]) == 0 {
			continue
		}
		for _, v := range frame.Data.Values[0] {
			if s, ok := v.(string); ok && !seen[s] {
				seen[s] = true
				result.TagKeys = append(result.TagKeys, s)
			}
		}
	}

	return result
}

func extractTagValues(grafanaResp *GrafanaQueryResponse) *TagValuesResponse { //nolint:dupl
	result := &TagValuesResponse{
		Values: []TagValue{},
	}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok || len(grafanaResult.Frames) == 0 {
		return result
	}

	// SHOW TAG VALUES returns two columns: tag key and tag value.
	for _, frame := range grafanaResult.Frames {
		if len(frame.Data.Values) < 2 {
			continue
		}
		rowCount := len(frame.Data.Values[0])
		for i := range rowCount {
			var key, value string
			if i < len(frame.Data.Values[0]) {
				if s, ok := frame.Data.Values[0][i].(string); ok {
					key = s
				}
			}
			if i < len(frame.Data.Values[1]) {
				if s, ok := frame.Data.Values[1][i].(string); ok {
					value = s
				}
			}
			result.Values = append(result.Values, TagValue{Key: key, Value: value})
		}
	}

	return result
}

func convertGrafanaResponse(grafanaResp *GrafanaQueryResponse) *QueryResponse {
	result := &QueryResponse{}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok || len(grafanaResult.Frames) == 0 {
		return result
	}

	// Use the first frame to establish base column names and detect time columns.
	firstFrame := grafanaResult.Frames[0]
	numBaseCols := len(firstFrame.Schema.Fields)
	baseColumns := make([]string, numBaseCols)
	timeColumns := make(map[int]bool)
	for i, field := range firstFrame.Schema.Fields {
		baseColumns[i] = field.Name
		if field.Type == "time" {
			timeColumns[i] = true
		}
	}

	// Collect the union of all label keys across all frames and fields.
	// Flux returns one frame per series; labels on fields identify the series (e.g. host, region).
	labelKeySet := make(map[string]bool)
	for _, frame := range grafanaResult.Frames {
		for _, field := range frame.Schema.Fields {
			for key := range field.Labels {
				labelKeySet[key] = true
			}
		}
	}
	labelKeys := make([]string, 0, len(labelKeySet))
	for key := range labelKeySet {
		labelKeys = append(labelKeys, key)
	}
	sort.Strings(labelKeys)

	// Build final column list: base columns + sorted label columns.
	totalCols := numBaseCols + len(labelKeys)
	columns := make([]string, totalCols)
	copy(columns, baseColumns)
	for i, key := range labelKeys {
		columns[numBaseCols+i] = key
	}
	result.Columns = columns
	result.TimeColumns = timeColumns

	// Collect rows from all frames, appending label values as extra columns.
	for _, frame := range grafanaResult.Frames {
		if len(frame.Data.Values) == 0 || len(frame.Data.Values[0]) == 0 {
			continue
		}

		// Merge all field labels for this frame into a single lookup map.
		frameLabelValues := make(map[string]string, len(labelKeys))
		for _, field := range frame.Schema.Fields {
			maps.Copy(frameLabelValues, field.Labels)
		}

		rowCount := len(frame.Data.Values[0])
		for rowIdx := range rowCount {
			row := make([]any, totalCols)
			for colIdx := range numBaseCols {
				if colIdx < len(frame.Data.Values) && rowIdx < len(frame.Data.Values[colIdx]) {
					row[colIdx] = frame.Data.Values[colIdx][rowIdx]
				}
			}
			for i, key := range labelKeys {
				row[numBaseCols+i] = frameLabelValues[key]
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

func extractFieldKeys(grafanaResp *GrafanaQueryResponse) *FieldKeysResponse { //nolint:dupl
	result := &FieldKeysResponse{
		Fields: []FieldKey{},
	}

	grafanaResult, ok := grafanaResp.Results["A"]
	if !ok || len(grafanaResult.Frames) == 0 {
		return result
	}

	for _, frame := range grafanaResult.Frames {
		if len(frame.Data.Values) < 2 {
			continue
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
	}

	return result
}
