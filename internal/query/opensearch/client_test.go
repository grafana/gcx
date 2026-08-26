package opensearch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/opensearch"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *opensearch.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := opensearch.NewClient(cfg)
	require.NoError(t, err)
	return client
}

// firstMetric returns the first metrics entry of a captured query object.
func firstMetric(t *testing.T, q map[string]any) map[string]any {
	t.Helper()
	metrics, ok := q["metrics"].([]any)
	require.True(t, ok)
	require.NotEmpty(t, metrics)
	m, ok := metrics[0].(map[string]any)
	require.True(t, ok)
	return m
}

func searchReq() opensearch.SearchRequest {
	return opensearch.SearchRequest{
		Query: "level:error",
		Size:  50,
		Start: time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC),
	}
}

// capture returns the first query object of the request the client sent.
func capture(t *testing.T, run func(c *opensearch.Client) error) map[string]any {
	t.Helper()
	var (
		captured   map[string]any
		decodedErr error
	)
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decodedErr = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
	}))
	require.NoError(t, run(client))
	require.NoError(t, decodedErr)

	queries, ok := captured["queries"].([]any)
	require.True(t, ok)
	require.Len(t, queries, 1)
	q, ok := queries[0].(map[string]any)
	require.True(t, ok)
	return q
}

func TestSearch(t *testing.T) {
	t.Run("parses documents with time conversion", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"@timestamp","type":"time"},{"name":"app","type":"string"}]},"data":{"values":[[1752451200000],["frontend"]]}}]}}}`))
		}))

		resp, err := client.Search(context.Background(), "test-uid", searchReq())
		require.NoError(t, err)
		require.Len(t, resp.Rows, 1)
		assert.Equal(t, "2025-07-14T00:00:00Z", resp.Rows[0][0])
		assert.Equal(t, "frontend", resp.Rows[0][1])
	})

	// Pins the wire shape: Lucene string in "query", raw_data metric with a
	// string size, default @timestamp timeField, and intervalMs/maxDataPoints
	// present (their absence causes "too many buckets" errors).
	t.Run("request body shape", func(t *testing.T) {
		q := capture(t, func(c *opensearch.Client) error {
			_, err := c.Search(context.Background(), "test-uid", searchReq())
			return err
		})

		assert.Equal(t, "level:error", q["query"])
		assert.Equal(t, "@timestamp", q["timeField"])
		require.NotNil(t, q["intervalMs"])
		require.NotNil(t, q["maxDataPoints"])

		metrics, ok := q["metrics"].([]any)
		require.True(t, ok)
		require.Len(t, metrics, 1)
		m, ok := metrics[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "raw_data", m["type"])
		settings, ok := m["settings"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "50", settings["size"])

		ds, ok := q["datasource"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "grafana-opensearch-datasource", ds["type"])
	})

	t.Run("custom --step is sent as intervalMs", func(t *testing.T) {
		req := searchReq()
		req.StepMs = 300_000
		q := capture(t, func(c *opensearch.Client) error {
			_, err := c.Search(context.Background(), "test-uid", req)
			return err
		})
		intervalMs, ok := q["intervalMs"].(float64)
		require.True(t, ok, "intervalMs must be a JSON number, got %T", q["intervalMs"])
		assert.InDelta(t, 300_000, intervalMs, 0.5)
	})

	t.Run("error envelope returns typed API error", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"error":"Failed to parse query [level:[unclosed]","status":400}}}`))
		}))

		_, err := client.Search(context.Background(), "test-uid", searchReq())
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "opensearch", apiErr.Datasource)
		assert.Contains(t, apiErr.Message, "Failed to parse query")
	})
}

func TestLogs(t *testing.T) {
	t.Run("trims plugin-internal fields", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"@timestamp","type":"time"},{"name":"_source","type":"other"},{"name":"message","type":"string"},{"name":"sort","type":"other"},{"name":"highlight","type":"other"}]},"data":{"values":[[1752451200000],[{"a":1}],["boom"],[[1,2]],[null]]}}]}}}`))
		}))

		resp, err := client.Logs(context.Background(), "test-uid", searchReq())
		require.NoError(t, err)
		require.Len(t, resp.Columns, 2)
		assert.Equal(t, "@timestamp", resp.Columns[0].Name)
		assert.Equal(t, "message", resp.Columns[1].Name)
		require.Len(t, resp.Rows, 1)
		assert.Equal(t, "boom", resp.Rows[0][1])
	})

	t.Run("uses logs metric with limit", func(t *testing.T) {
		q := capture(t, func(c *opensearch.Client) error {
			_, err := c.Logs(context.Background(), "test-uid", searchReq())
			return err
		})
		m := firstMetric(t, q)
		assert.Equal(t, "logs", m["type"])
		settings, ok := m["settings"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "50", settings["limit"])
	})
}

func TestAggregations(t *testing.T) {
	aggsReq := opensearch.AggsRequest{
		Query:   "*",
		Agg:     "count",
		GroupBy: "app.keyword",
		Start:   time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		End:     time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC),
	}

	// Pins the wire shape: terms bucket before date_histogram, string sizes,
	// and min_doc_count 1 (tabular output drops empty buckets).
	t.Run("request body shape", func(t *testing.T) {
		q := capture(t, func(c *opensearch.Client) error {
			_, err := c.Aggregations(context.Background(), "test-uid", aggsReq)
			return err
		})

		bucketAggs, ok := q["bucketAggs"].([]any)
		require.True(t, ok)
		require.Len(t, bucketAggs, 2)

		terms, ok := bucketAggs[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "terms", terms["type"])
		assert.Equal(t, "app.keyword", terms["field"])

		hist, ok := bucketAggs[1].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "date_histogram", hist["type"])
		// aggsReq leaves TimeField unset, and it must reach the plugin unset
		// too: the plugin uses the datasource's own configured time field
		// when this is empty, matching the field it already uses to build
		// the range filter. Defaulting it here would bucket on "@timestamp"
		// while the filter uses the datasource's real field.
		assert.Empty(t, hist["field"])
		settings, ok := hist["settings"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "1", settings["min_doc_count"])

		m := firstMetric(t, q)
		assert.Equal(t, "count", m["type"])
		_, hasField := m["field"]
		assert.False(t, hasField, "count must not carry a field")
	})

	t.Run("explicit time field overrides the histogram bucket", func(t *testing.T) {
		req := aggsReq
		req.TimeField = "event.time"

		q := capture(t, func(c *opensearch.Client) error {
			_, err := c.Aggregations(context.Background(), "test-uid", req)
			return err
		})

		bucketAggs, ok := q["bucketAggs"].([]any)
		require.True(t, ok)
		require.Len(t, bucketAggs, 2)

		hist, ok := bucketAggs[1].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "event.time", hist["field"])
	})

	t.Run("parses group frames into named series", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[
				{"schema":{"name":"tempo","fields":[{"name":"Time","type":"time"},{"name":"Value","type":"number"}]},"data":{"values":[[1752451200000],[8]]}},
				{"schema":{"name":"faro","fields":[{"name":"Time","type":"time"},{"name":"Value","type":"number"}]},"data":{"values":[[1752451200000],[null]]}}
			]}}}`))
		}))

		resp, err := client.Aggregations(context.Background(), "test-uid", aggsReq)
		require.NoError(t, err)
		require.Len(t, resp.Series, 2)
		assert.Equal(t, "tempo", resp.Series[0].Name)
		require.NotNil(t, resp.Series[0].Values[0])
		assert.InDelta(t, 8.0, *resp.Series[0].Values[0], 0.001)
		assert.Equal(t, time.Date(2025, 7, 14, 0, 0, 0, 0, time.UTC), resp.Series[0].Timestamps[0])
		// Null buckets stay nil (gaps), not zero.
		assert.Equal(t, "faro", resp.Series[1].Name)
		assert.Nil(t, resp.Series[1].Values[0])
	})
}

func TestMapping(t *testing.T) {
	t.Run("fetches and parses mappings", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/datasources/uid/test-uid/resources/_mapping", r.URL.Path)
			_, _ = w.Write([]byte(`{"logs-a":{"mappings":{"properties":{"@timestamp":{"type":"date"},"tags":{"properties":{"app":{"type":"keyword"}}}}}}}`))
		}))

		indices, fields, err := client.Mapping(context.Background(), "test-uid", "")
		require.NoError(t, err)
		require.Len(t, indices, 1)
		assert.Equal(t, opensearch.IndexInfo{Name: "logs-a", Fields: 2}, indices[0])
		require.Len(t, fields, 2)
		assert.Equal(t, opensearch.FieldInfo{Index: "logs-a", Name: "@timestamp", Type: "date"}, fields[0])
		assert.Equal(t, opensearch.FieldInfo{Index: "logs-a", Name: "tags.app", Type: "keyword"}, fields[1])
	})

	t.Run("index scoping and error propagation", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/datasources/uid/test-uid/resources/logs-a/_mapping", r.URL.Path)
			http.Error(w, `{"message":"index_not_found_exception"}`, http.StatusNotFound)
		}))

		_, _, err := client.Mapping(context.Background(), "test-uid", "logs-a")
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	})
}
