package elasticsearch_test

import (
	"encoding/json"
	"net/url"
	"testing"

	dselastic "github.com/grafana/gcx/internal/datasources/elasticsearch"
	dsquery "github.com/grafana/gcx/internal/datasources/query"
	"github.com/grafana/gcx/internal/query/elasticsearch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testHost = "https://example.grafana.net"
	testUID  = "es-uid"
)

func testBase() dsquery.ExploreQuery {
	return dsquery.ExploreQuery{
		DatasourceUID:  testUID,
		DatasourceType: "elasticsearch",
		From:           "now-6h",
		To:             "now",
		OrgID:          1,
	}
}

// decodePane parses an Explore URL and returns the single pane and its first query.
func decodePane(t *testing.T, rawURL string) (map[string]any, map[string]any) {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	require.NoError(t, err)
	assert.Equal(t, "/explore", parsed.Path)

	params := parsed.Query()
	assert.Equal(t, "1", params.Get("schemaVersion"))
	assert.Equal(t, "1", params.Get("orgId"))

	var panes map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(params.Get("panes")), &panes))

	pane, ok := panes[dsquery.DefaultExplorePaneID]
	require.True(t, ok, "expected pane %q", dsquery.DefaultExplorePaneID)
	assert.Equal(t, testUID, pane["datasource"])

	queries, ok := pane["queries"].([]any)
	require.True(t, ok)
	require.Len(t, queries, 1)

	query, ok := queries[0].(map[string]any)
	require.True(t, ok)
	return pane, query
}

// assertCommonQueryFields checks the fields every Elasticsearch query model carries.
func assertCommonQueryFields(t *testing.T, pane, query map[string]any) {
	t.Helper()

	assert.Equal(t, "A", query["refId"])
	assert.Equal(t, map[string]any{"type": "elasticsearch", "uid": testUID}, query["datasource"])
	assert.InDelta(t, 1000, query["maxDataPoints"], 0)

	// from/to live on the pane range, never inside the query model.
	assert.NotContains(t, query, "from")
	assert.NotContains(t, query, "to")
	assert.Equal(t, map[string]any{"from": "now-6h", "to": "now"}, pane["range"])
}

func TestQueryExploreURL(t *testing.T) {
	t.Run("mirrors the raw_data search model", func(t *testing.T) {
		got := dselastic.QueryExploreURL(testHost, testBase(), elasticsearch.SearchRequest{
			Query:     "level:error",
			Size:      20,
			TimeField: "@timestamp",
		})
		require.NotEmpty(t, got)

		pane, query := decodePane(t, got)
		assertCommonQueryFields(t, pane, query)

		assert.Equal(t, "level:error", query["query"])
		assert.Equal(t, "@timestamp", query["timeField"])
		assert.Equal(t, []any{}, query["bucketAggs"])
		assert.Equal(t, []any{map[string]any{
			"id":       "1",
			"type":     "raw_data",
			"settings": map[string]any{"size": "20"},
		}}, query["metrics"])
	})

	t.Run("keeps an empty expression, which matches all documents", func(t *testing.T) {
		got := dselastic.QueryExploreURL(testHost, testBase(), elasticsearch.SearchRequest{Size: 100})
		require.NotEmpty(t, got)

		_, query := decodePane(t, got)
		assert.Contains(t, query, "query")
		assert.Empty(t, query["query"])
		assert.Equal(t, "@timestamp", query["timeField"], "defaults to the conventional time field")
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		req := elasticsearch.SearchRequest{Query: "level:error", Size: 20}
		assert.Empty(t, dselastic.QueryExploreURL("", testBase(), req))
		assert.Empty(t, dselastic.QueryExploreURL("   ", testBase(), req))
		assert.Empty(t, dselastic.QueryExploreURL(testHost, dsquery.ExploreQuery{DatasourceType: "elasticsearch"}, req))
	})
}

func TestLogsExploreURL(t *testing.T) {
	t.Run("mirrors the logs search model", func(t *testing.T) {
		got := dselastic.LogsExploreURL(testHost, testBase(), elasticsearch.SearchRequest{
			Query:     "level:error",
			Size:      50,
			TimeField: "event.time",
		})
		require.NotEmpty(t, got)

		pane, query := decodePane(t, got)
		assertCommonQueryFields(t, pane, query)

		assert.Equal(t, "level:error", query["query"])
		assert.Equal(t, "event.time", query["timeField"])
		assert.Equal(t, []any{}, query["bucketAggs"])
		assert.Equal(t, []any{map[string]any{
			"id":       "1",
			"type":     "logs",
			"settings": map[string]any{"limit": "50"},
		}}, query["metrics"])

		// Explore should show newest lines first, like the CLI output does.
		assert.Equal(t, map[string]any{
			"logs": map[string]any{"sortOrder": "Descending"},
		}, pane["panelsState"])
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		req := elasticsearch.SearchRequest{Query: "level:error", Size: 50}
		assert.Empty(t, dselastic.LogsExploreURL("", testBase(), req))
		assert.Empty(t, dselastic.LogsExploreURL(testHost, dsquery.ExploreQuery{DatasourceType: "elasticsearch"}, req))
	})
}

func TestMetricsExploreURL(t *testing.T) {
	t.Run("mirrors the aggregation model with a terms split", func(t *testing.T) {
		got := dselastic.MetricsExploreURL(testHost, testBase(), elasticsearch.AggsRequest{
			Query:     "level:error",
			Agg:       "avg",
			Field:     "duration_ms",
			GroupBy:   "app.keyword",
			GroupSize: 5,
			TimeField: "@timestamp",
			StepMs:    60_000,
		})
		require.NotEmpty(t, got)

		pane, query := decodePane(t, got)
		assertCommonQueryFields(t, pane, query)

		assert.Equal(t, "level:error", query["query"])
		assert.InDelta(t, 60_000, query["intervalMs"], 0)
		assert.Equal(t, []any{map[string]any{
			"id":    "1",
			"type":  "avg",
			"field": "duration_ms",
		}}, query["metrics"])
		assert.Equal(t, []any{
			map[string]any{
				"id":    "3",
				"type":  "terms",
				"field": "app.keyword",
				"settings": map[string]any{
					"size":    "5",
					"order":   "desc",
					"orderBy": "_count",
				},
			},
			map[string]any{
				"id":       "2",
				"type":     "date_histogram",
				"field":    "@timestamp",
				"settings": map[string]any{"interval": "auto", "min_doc_count": "1"},
			},
		}, query["bucketAggs"])
	})

	t.Run("mirrors an ungrouped count aggregation", func(t *testing.T) {
		got := dselastic.MetricsExploreURL(testHost, testBase(), elasticsearch.AggsRequest{
			Agg:    "count",
			StepMs: 10_000,
		})
		require.NotEmpty(t, got)

		_, query := decodePane(t, got)
		assert.Equal(t, []any{map[string]any{"id": "1", "type": "count"}}, query["metrics"])

		bucketAggs, ok := query["bucketAggs"].([]any)
		require.True(t, ok)
		require.Len(t, bucketAggs, 1, "no terms bucket without --group-by")
		bucket, ok := bucketAggs[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "date_histogram", bucket["type"])
	})

	t.Run("returns empty for missing required fields", func(t *testing.T) {
		req := elasticsearch.AggsRequest{Agg: "count"}
		assert.Empty(t, dselastic.MetricsExploreURL("", testBase(), req))
		assert.Empty(t, dselastic.MetricsExploreURL(testHost, dsquery.ExploreQuery{DatasourceType: "elasticsearch"}, req))
	})
}
