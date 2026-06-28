package athena

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

// Client executes queries and resource requests against an Athena datasource via Grafana.
type Client struct {
	host        string
	httpClient  *http.Client
	queryClient *grafanaquery.Client
}

// NewClient creates a new Athena query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{
		host:        cfg.Host,
		httpClient:  httpClient,
		queryClient: grafanaquery.NewClientWithHTTPClient(cfg, httpClient),
	}, nil
}

// Resource makes a POST request to the Athena plugin's resource API for schema discovery.
func (c *Client) Resource(ctx context.Context, datasourceUID string, path string, body map[string]string) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal resource request: %w", err)
	}

	resourceURL := fmt.Sprintf("%s/api/datasources/uid/%s/resources%s", c.host, url.PathEscape(datasourceUID), path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resourceURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := httputils.ReadResponseBody(resp.Body, httputils.DefaultResponseLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, queryerror.FromBody("athena", "resource"+path, resp.StatusCode, respBody)
	}

	return respBody, nil
}

// Query executes a SQL query against the specified Athena datasource.
func (c *Client) Query(ctx context.Context, datasourceUID string, req QueryRequest) (*QueryResponse, error) {
	from := strconv.FormatInt(req.Start.UnixMilli(), 10)
	to := strconv.FormatInt(req.End.UnixMilli(), 10)
	if req.Start.IsZero() || req.End.IsZero() {
		now := time.Now()
		from = strconv.FormatInt(now.Add(-1*time.Hour).UnixMilli(), 10)
		to = strconv.FormatInt(now.UnixMilli(), 10)
	}

	connectionArgs := map[string]any{}
	if req.Region != "" {
		connectionArgs["region"] = req.Region
	}
	if req.Catalog != "" {
		connectionArgs["catalog"] = req.Catalog
	}
	if req.Database != "" {
		connectionArgs["database"] = req.Database
	}
	if req.ResultReuseEnabled {
		connectionArgs["resultReuseEnabled"] = true
		if req.ResultReuseMaxAgeInMinutes > 0 {
			connectionArgs["resultReuseMaxAgeInMinutes"] = req.ResultReuseMaxAgeInMinutes
		}
	}

	queryMap := map[string]any{
		"refId":      "A",
		"datasource": map[string]any{"type": DatasourceType, "uid": datasourceUID},
		"rawSql":     req.RawSQL,
		"format":     QueryFormatTable,
	}
	if len(connectionArgs) > 0 {
		queryMap["connectionArgs"] = connectionArgs
	}

	bodyMap := map[string]any{
		"queries": []any{queryMap},
		"from":    from,
		"to":      to,
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respBody, err := c.queryClient.Execute(ctx, body, "athena", "query")
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
		return nil, queryerror.New("athena", "query", status, result.Error, result.ErrorSource)
	}

	resp := &QueryResponse{}

	// Athena uses grafana/sqlds which returns exactly one frame per query result set.
	// See: github.com/grafana/sqlds/blob/5b4920612a218a63954c7f9f34947c63d2bff19b/query.go#L252
	// Ref: github.com/grafana/athena-datasource/blob/e0876565e2a37d28b3ecc2c1d213fb1c5fa66f3b/pkg/athena/datasource.go#L21
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
