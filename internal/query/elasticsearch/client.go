package elasticsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/httputils"
	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/query/grafanaquery"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/grafana/gcx/internal/queryerror"
	"k8s.io/client-go/rest"
)

const (
	maxResourceResponseBytes = 4 << 20 // _mapping responses can be large on busy clusters

	// pluginID is the Grafana Elasticsearch datasource plugin ID.
	pluginID = "elasticsearch"

	// DefaultTimeField is the Elasticsearch datasource's conventional time field.
	DefaultTimeField = "@timestamp"
)

// Client executes Elasticsearch queries and mapping discovery via Grafana's
// datasource APIs.
type Client struct {
	restConfig  config.NamespacedRESTConfig
	httpClient  *http.Client
	queryClient *grafanaquery.Client
}

// NewClient creates a new Elasticsearch query client.
func NewClient(cfg config.NamespacedRESTConfig) (*Client, error) {
	httpClient, err := rest.HTTPClientFor(&cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}
	return &Client{
		restConfig:  cfg,
		httpClient:  httpClient,
		queryClient: grafanaquery.NewClientWithHTTPClient(cfg, httpClient),
	}, nil
}

// Search executes a Lucene query returning matching documents (raw_data).
func (c *Client) Search(ctx context.Context, dsUID string, req SearchRequest) (*querysql.QueryResponse, error) {
	metric := map[string]any{
		"id":       "1",
		"type":     "raw_data",
		"settings": map[string]any{"size": strconv.Itoa(req.Size)},
	}
	resp, err := c.runQuery(ctx, dsUID, "query", req.Query, req.TimeField, metric, nil, req.Start, req.End, 60_000)
	if err != nil {
		return nil, err
	}
	return convertTimeColumns(resp), nil
}

// Logs executes a Lucene query using the plugin's logs metric type: newest
// documents first, with log-oriented field handling.
func (c *Client) Logs(ctx context.Context, dsUID string, req SearchRequest) (*querysql.QueryResponse, error) {
	metric := map[string]any{
		"id":       "1",
		"type":     "logs",
		"settings": map[string]any{"limit": strconv.Itoa(req.Size)},
	}
	resp, err := c.runQuery(ctx, dsUID, "logs", req.Query, req.TimeField, metric, nil, req.Start, req.End, 60_000)
	if err != nil {
		return nil, err
	}
	return convertTimeColumns(trimLogsColumns(resp)), nil
}

// Aggregations executes a metric aggregation bucketed by a date histogram and
// optionally split by a terms group, one series per group.
func (c *Client) Aggregations(ctx context.Context, dsUID string, req AggsRequest) (*MetricsResponse, error) {
	metric := map[string]any{"id": "1", "type": req.Agg}
	if req.Field != "" {
		metric["field"] = req.Field
	}

	bucketAggs := []any{}
	if req.GroupBy != "" {
		size := req.GroupSize
		if size <= 0 {
			size = 10
		}
		bucketAggs = append(bucketAggs, map[string]any{
			"id":    "3",
			"type":  "terms",
			"field": req.GroupBy,
			"settings": map[string]any{
				"size":    strconv.Itoa(size),
				"order":   "desc",
				"orderBy": "_count",
			},
		})
	}
	bucketAggs = append(bucketAggs, map[string]any{
		"id":    "2",
		"type":  "date_histogram",
		"field": orDefault(req.TimeField, DefaultTimeField),
		// min_doc_count 1 drops empty buckets: tabular output should show
		// where data is, not zero-fill the whole range like a chart would.
		"settings": map[string]any{"interval": "auto", "min_doc_count": "1"},
	})

	stepMs := req.StepMs
	if stepMs == 0 {
		stepMs = intervalMsFor(req.Start, req.End)
	}

	body, err := c.executeQuery(ctx, dsUID, "metrics", req.Query, req.TimeField, metric, bucketAggs, req.Start, req.End, stepMs)
	if err != nil {
		return nil, err
	}
	return parseAggsResponse(body)
}

// runQuery executes a single-metric query and parses the first frame as a table.
func (c *Client) runQuery(ctx context.Context, dsUID, operation, query, timeField string, metric map[string]any, bucketAggs []any, start, end time.Time, stepMs int64) (*querysql.QueryResponse, error) {
	body, err := c.executeQuery(ctx, dsUID, operation, query, timeField, metric, bucketAggs, start, end, stepMs)
	if err != nil {
		return nil, err
	}
	return querysql.ParseResponse(body, "elasticsearch")
}

func (c *Client) executeQuery(ctx context.Context, dsUID, operation, query, timeField string, metric map[string]any, bucketAggs []any, start, end time.Time, stepMs int64) ([]byte, error) {
	if bucketAggs == nil {
		bucketAggs = []any{}
	}
	q := map[string]any{
		"refId":      "A",
		"datasource": map[string]any{"type": pluginID, "uid": dsUID},
		"query":      query,
		"metrics":    []any{metric},
		"bucketAggs": bucketAggs,
		"timeField":  orDefault(timeField, DefaultTimeField),
		// The plugin derives histogram bucket sizing from intervalMs and
		// maxDataPoints; omitting them causes "too many buckets" errors from
		// Elasticsearch on wide time ranges.
		"intervalMs":    stepMs,
		"maxDataPoints": 1000,
	}

	bodyMap := map[string]any{
		"queries": []any{q},
		"from":    strconv.FormatInt(start.UnixMilli(), 10),
		"to":      strconv.FormatInt(end.UnixMilli(), 10),
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	return c.queryClient.Execute(ctx, body, "elasticsearch", operation)
}

// parseAggsResponse converts per-group time-series frames into MetricSeries;
// the group value is carried on the frame name.
func parseAggsResponse(body []byte) (*MetricsResponse, error) {
	var raw dataframe.Response
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	result, ok := raw.Results["A"]
	if !ok {
		return &MetricsResponse{}, nil
	}
	if result.Error != "" {
		status := result.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		return nil, queryerror.New("elasticsearch", "metrics", status, result.Error, result.ErrorSource)
	}

	resp := &MetricsResponse{}
	for _, frame := range result.Frames {
		if len(frame.Data.Values) < 2 {
			continue
		}
		times, values := frame.Data.Values[0], frame.Data.Values[1]
		n := min(len(times), len(values))
		series := MetricSeries{}
		series.Name = frame.Schema.Name
		for i := range n {
			ms, ok := times[i].(float64)
			if !ok {
				continue
			}
			series.Timestamps = append(series.Timestamps, time.UnixMilli(int64(ms)).UTC())
			if v, ok := toFloat64Ptr(values[i]); ok {
				series.Values = append(series.Values, v)
			} else {
				series.Values = append(series.Values, nil)
			}
		}
		if len(series.Timestamps) > 0 {
			resp.Series = append(resp.Series, series)
		}
	}
	return resp, nil
}

// toFloat64Ptr converts a JSON number to *float64; nil values stay nil (gaps).
func toFloat64Ptr(v any) (*float64, bool) {
	if v == nil {
		return nil, true
	}
	if f, ok := v.(float64); ok {
		return &f, true
	}
	return nil, false
}

// Mapping fetches index mappings via the plugin resource proxy. index may be
// empty (all indices) or an index name/pattern.
func (c *Client) Mapping(ctx context.Context, dsUID, index string) ([]IndexInfo, []FieldInfo, error) {
	path := "_mapping"
	if index != "" {
		path = url.PathEscape(index) + "/_mapping"
	}

	fullPath := fmt.Sprintf("/api/datasources/uid/%s/resources/%s", url.PathEscape(dsUID), path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.restConfig.Host+fullPath, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to call %s: %w", fullPath, err)
	}
	defer resp.Body.Close()

	body, err := httputils.ReadResponseBody(resp.Body, maxResourceResponseBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, queryerror.FromBody("elasticsearch", "mapping", resp.StatusCode, body)
	}

	return ParseMapping(body)
}

// intervalMsFor derives a histogram interval from the time range targeting
// ~1000 buckets, with a 10s floor.
func intervalMsFor(start, end time.Time) int64 {
	const minIntervalMs = 10_000
	rangeMs := end.Sub(start).Milliseconds()
	if rangeMs <= 0 {
		return minIntervalMs
	}
	interval := rangeMs / 1000
	if interval < minIntervalMs {
		return minIntervalMs
	}
	return interval
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
