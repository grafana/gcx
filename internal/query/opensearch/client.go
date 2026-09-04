package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
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

	// pluginID is the Grafana OpenSearch datasource plugin ID.
	pluginID = "grafana-opensearch-datasource"

	// DefaultTimeField is the OpenSearch datasource's conventional time field.
	DefaultTimeField = "@timestamp"
)

// Client executes OpenSearch queries and mapping discovery via Grafana's
// datasource APIs.
type Client struct {
	restConfig  config.NamespacedRESTConfig
	httpClient  *http.Client
	queryClient *grafanaquery.Client
}

// NewClient creates a new OpenSearch query client.
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

// SearchQueryModel returns the OpenSearch plugin query model for a document
// search (the raw_data metric type).
//
// Client.Search and the Explore link builder both call this, so the query the
// CLI sends and the query Explore opens cannot drift apart. The time range is
// not part of the model: the request body carries it for the client, and the
// pane range carries it for Explore.
func SearchQueryModel(dsUID string, req SearchRequest) map[string]any {
	metric := map[string]any{
		"id":       "1",
		"type":     "raw_data",
		"settings": map[string]any{"size": strconv.Itoa(req.Size)},
	}
	return queryModel(dsUID, req.Query, req.TimeField, metric, nil, orDefaultStep(req.StepMs))
}

// LogsQueryModel returns the OpenSearch plugin query model for a log search
// (the logs metric type). Client.Logs and the Explore link builder both call it.
//
// The logs metric's row-count setting is named "size" here, not "limit" as in
// Elasticsearch's plugin — live-verified against a real OpenSearch datasource:
// "limit" is silently ignored (the backend falls back to its own default of
// 500 regardless of the requested value), while "size" is honored exactly.
// This is the one place OpenSearch's wire protocol actually diverges from
// Elasticsearch's; every other query shape (raw_data, aggregations, mapping
// discovery) uses identical field names on both plugins.
func LogsQueryModel(dsUID string, req SearchRequest) map[string]any {
	metric := map[string]any{
		"id":       "1",
		"type":     "logs",
		"settings": map[string]any{"size": strconv.Itoa(req.Size)},
	}
	return queryModel(dsUID, req.Query, req.TimeField, metric, nil, orDefaultStep(req.StepMs))
}

// AggsQueryModel returns the OpenSearch plugin query model for a metric
// aggregation over a date histogram, optionally split by a terms bucket.
// Client.Aggregations and the Explore link builder both call it.
func AggsQueryModel(dsUID string, req AggsRequest) map[string]any {
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
		"id":   "2",
		"type": "date_histogram",
		// Left as req.TimeField, not defaulted: an unset --time-field must
		// reach the plugin empty so it buckets on the datasource's own
		// configured time field, the same field it already uses to build the
		// range filter. Defaulting here to DefaultTimeField would bucket on
		// "@timestamp" while filtering on the datasource's real field,
		// producing empty or wrong series whenever they differ.
		"field": req.TimeField,
		// min_doc_count 1 drops empty buckets: tabular output should show
		// where data is, not zero-fill the whole range like a chart would.
		"settings": map[string]any{"interval": "auto", "min_doc_count": "1"},
	})

	stepMs := req.StepMs
	if stepMs == 0 {
		stepMs = intervalMsFor(req.Start, req.End)
	}

	return queryModel(dsUID, req.Query, req.TimeField, metric, bucketAggs, stepMs)
}

// queryModel assembles the fields every OpenSearch query model shares.
func queryModel(dsUID, query, timeField string, metric map[string]any, bucketAggs []any, stepMs int64) map[string]any {
	if bucketAggs == nil {
		bucketAggs = []any{}
	}
	return map[string]any{
		"refId":      "A",
		"datasource": map[string]any{"type": pluginID, "uid": dsUID},
		"query":      query,
		"metrics":    []any{metric},
		"bucketAggs": bucketAggs,
		"timeField":  orDefault(timeField, DefaultTimeField),
		// The plugin derives histogram bucket sizing from intervalMs and
		// maxDataPoints; omitting them causes "too many buckets" errors from
		// OpenSearch on wide time ranges.
		"intervalMs":    stepMs,
		"maxDataPoints": 1000,
	}
}

// Search executes a Lucene query returning matching documents (raw_data).
func (c *Client) Search(ctx context.Context, dsUID string, req SearchRequest) (*querysql.QueryResponse, error) {
	resp, err := c.runQuery(ctx, "query", SearchQueryModel(dsUID, req), req.Start, req.End)
	if err != nil {
		return nil, err
	}
	return convertTimeColumns(resp), nil
}

// Logs executes a Lucene query using the plugin's logs metric type: newest
// documents first, with log-oriented field handling.
func (c *Client) Logs(ctx context.Context, dsUID string, req SearchRequest) (*querysql.QueryResponse, error) {
	resp, err := c.runQuery(ctx, "logs", LogsQueryModel(dsUID, req), req.Start, req.End)
	if err != nil {
		return nil, err
	}
	return convertTimeColumns(trimLogsColumns(resp)), nil
}

// Aggregations executes a metric aggregation bucketed by a date histogram and
// optionally split by a terms group, one series per group.
func (c *Client) Aggregations(ctx context.Context, dsUID string, req AggsRequest) (*MetricsResponse, error) {
	body, err := c.executeQuery(ctx, "metrics", AggsQueryModel(dsUID, req), req.Start, req.End)
	if err != nil {
		return nil, err
	}
	return parseAggsResponse(body)
}

// runQuery executes a single-metric query and parses the first frame as a table.
func (c *Client) runQuery(ctx context.Context, operation string, q map[string]any, start, end time.Time) (*querysql.QueryResponse, error) {
	body, err := c.executeQuery(ctx, operation, q, start, end)
	if err != nil {
		return nil, err
	}
	return querysql.ParseResponse(body, "opensearch")
}

func (c *Client) executeQuery(ctx context.Context, operation string, q map[string]any, start, end time.Time) ([]byte, error) {
	bodyMap := map[string]any{
		"queries": []any{q},
		"from":    strconv.FormatInt(start.UnixMilli(), 10),
		"to":      strconv.FormatInt(end.UnixMilli(), 10),
	}

	body, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	return c.queryClient.Execute(ctx, body, "opensearch", operation)
}

// parseAggsResponse converts per-group time-series frames into MetricSeries.
// The group value's location differs by plugin: Elasticsearch's names the
// frame itself (frame.Schema.Name); OpenSearch's leaves that empty and
// instead labels the value field, e.g. {"app.keyword": "grafana"} — live-
// verified against a real OpenSearch datasource. seriesName tries the frame
// name first and falls back to the value field's labels, so this works
// against either plugin's shape without needing to know which one sent it.
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
		return nil, queryerror.New("opensearch", "metrics", status, result.Error, result.ErrorSource)
	}

	resp := &MetricsResponse{}
	for _, frame := range result.Frames {
		if len(frame.Data.Values) < 2 {
			continue
		}
		times, values := frame.Data.Values[0], frame.Data.Values[1]
		n := min(len(times), len(values))
		series := MetricSeries{}
		series.Name = seriesName(frame)
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

// seriesName derives a series name from a frame: the frame's own name if
// set (Elasticsearch's shape), otherwise a name synthesized from the labels
// on its value field, the second field (OpenSearch's shape — only one
// --group-by field is supported today, so that label map has at most one
// entry, but multiple are joined for safety if the plugin ever adds more).
func seriesName(frame dataframe.Frame) string {
	if frame.Schema.Name != "" {
		return frame.Schema.Name
	}
	if len(frame.Schema.Fields) < 2 {
		return ""
	}
	labels := frame.Schema.Fields[1].Labels
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values := make([]string, len(keys))
	for i, k := range keys {
		values[i] = labels[k]
	}
	return strings.Join(values, ", ")
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
		return nil, nil, queryerror.FromBody("opensearch", "mapping", resp.StatusCode, body)
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

// orDefaultStep returns the histogram interval to send: the caller's step
// when set, otherwise a 60s default (intervalMs must always be present; its
// absence causes "too many buckets" errors from OpenSearch).
func orDefaultStep(stepMs int64) int64 {
	if stepMs > 0 {
		return stepMs
	}
	return 60_000
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
