package loki_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// streamsWithCategorizedLabels is the exact shape Loki's own HTTP API
// returns for a streams result when the categorize-labels encoding flag is
// set — verified against grafana/loki's pkg/querier/queryrange/codec_test.go
// fixture, not guessed.
const streamsWithCategorizedLabels = `{
	"status": "success",
	"data": {
		"resultType": "streams",
		"result": [
			{
				"stream": {"test": "test"},
				"values": [
					["123456789012345", "super line", {}],
					["123456789012346", "super line2", {"structuredMetadata": {"x": "a", "y": "b"}}],
					["123456789012347", "super line3 z=text", {"structuredMetadata": {"x": "a", "y": "b"}, "parsed": {"z": "text"}}]
				]
			}
		],
		"stats": {"summary": {"bytesProcessedPerSecond": 100}}
	}
}`

func newNativeTestClient(t *testing.T, handler http.HandlerFunc) *loki.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := loki.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestQueryNative_BuildsRangeRequest(t *testing.T) {
	var gotPath string
	var gotQuery url.Values
	var gotHeader string

	client := newNativeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		gotHeader = r.Header.Get("X-Loki-Response-Encoding-Flags")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	})

	start := time.Unix(0, 1705315800123456789)
	end := time.Unix(0, 1705315801123456789)
	_, err := client.QueryNative(context.Background(), "loki-uid", loki.QueryRequest{
		Query: `{app="x"}`,
		Start: start,
		End:   end,
		Step:  5 * time.Second,
		Limit: 50,
	})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/loki-uid/resources/query_range", gotPath)
	assert.Equal(t, `{app="x"}`, gotQuery.Get("query"))
	assert.Equal(t, "1705315800123456789", gotQuery.Get("start"))
	assert.Equal(t, "1705315801123456789", gotQuery.Get("end"))
	assert.Equal(t, "5000ms", gotQuery.Get("step"))
	assert.Equal(t, "50", gotQuery.Get("limit"))
	assert.Equal(t, "categorize-labels", gotHeader)
}

func TestQueryNative_BuildsInstantRequest(t *testing.T) {
	var gotPath string
	var gotQuery url.Values

	client := newNativeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"streams","result":[]}}`))
	})

	_, err := client.QueryNative(context.Background(), "loki-uid", loki.QueryRequest{
		Query: `{app="x"}`,
	})
	require.NoError(t, err)

	assert.Equal(t, "/api/datasources/uid/loki-uid/resources/query", gotPath)
	assert.Equal(t, `{app="x"}`, gotQuery.Get("query"))
	assert.NotEmpty(t, gotQuery.Get("time"))
	assert.Empty(t, gotQuery.Get("start"))
	assert.Empty(t, gotQuery.Get("end"))
}

func TestQueryNative_DecodesCategorizedLabelsTuple(t *testing.T) {
	client := newNativeTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(streamsWithCategorizedLabels))
	})

	resp, err := client.QueryNative(context.Background(), "loki-uid", loki.QueryRequest{
		Query: `{test="test"}`,
		Start: time.Unix(0, 100),
		End:   time.Unix(0, 200),
	})
	require.NoError(t, err)

	require.Len(t, resp.Data.Result, 1)
	stream := resp.Data.Result[0]
	assert.Equal(t, map[string]string{"test": "test"}, stream.Stream)
	require.Len(t, stream.Values, 3)

	assert.Equal(t, "123456789012345", stream.Values[0].Timestamp)
	assert.Equal(t, "super line", stream.Values[0].Line)
	assert.Empty(t, stream.Values[0].StructuredMetadata)
	assert.Empty(t, stream.Values[0].Parsed)

	assert.Equal(t, map[string]string{"x": "a", "y": "b"}, stream.Values[1].StructuredMetadata)
	assert.Empty(t, stream.Values[1].Parsed)

	assert.Equal(t, map[string]string{"x": "a", "y": "b"}, stream.Values[2].StructuredMetadata)
	assert.Equal(t, map[string]string{"z": "text"}, stream.Values[2].Parsed)

	require.NotNil(t, resp.Data.Stats)
	assert.Equal(t, int64(100), resp.Data.Stats.Summary.BytesProcessedPerSecond)
}

func TestQueryNative_ReturnsTypedAPIErrorOnFailure(t *testing.T) {
	client := newNativeTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"parse error"}`))
	})

	_, err := client.QueryNative(context.Background(), "loki-uid", loki.QueryRequest{Query: `{app="x"}`})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "loki", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
}

func TestMetricQueryNative_DecodesMatrixDirectly(t *testing.T) {
	client := newNativeTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "matrix",
				"result": [
					{"metric": {"app": "x"}, "values": [[1705315800.123, "5"], [1705315801.123, "6"]]}
				]
			}
		}`))
	})

	resp, err := client.MetricQueryNative(context.Background(), "loki-uid", loki.QueryRequest{
		Query: `rate({app="x"}[5m])`,
		Start: time.Unix(0, 1705315800123456789),
		End:   time.Unix(0, 1705315801123456789),
	})
	require.NoError(t, err)

	require.Len(t, resp.Data.Result, 1)
	assert.Equal(t, "matrix", resp.Data.ResultType)
	assert.Equal(t, map[string]string{"app": "x"}, resp.Data.Result[0].Metric)
	require.Len(t, resp.Data.Result[0].Values, 2)
	assert.InDelta(t, 1705315800.123, resp.Data.Result[0].Values[0][0], 0.001)
	assert.Equal(t, "5", resp.Data.Result[0].Values[0][1])
}
