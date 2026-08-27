package bigquery_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/bigquery"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, srvURL string) *bigquery.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srvURL},
		Namespace: "default",
	}
	client, err := bigquery.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestQuery_ParsesColumnarResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"x","type":"number"},{"name":"y","type":"number"}]},"data":{"values":[[1],[2]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	resp, err := client.Query(context.Background(), "bq-uid", bigquery.QueryRequest{RawSQL: "SELECT 1 AS x, 2 AS y"})
	require.NoError(t, err)
	require.Len(t, resp.Columns, 2)
	assert.Equal(t, "x", resp.Columns[0].Name)
	assert.Equal(t, "y", resp.Columns[1].Name)
	require.Len(t, resp.Rows, 1)
	assert.InDelta(t, 1, resp.Rows[0][0], 1e-9)
	assert.InDelta(t, 2, resp.Rows[0][1], 1e-9)
}

func TestQuery_RequestConstruction(t *testing.T) {
	var capturedPath, capturedMethod, capturedContentType string
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedContentType = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "bq-uid", bigquery.QueryRequest{RawSQL: "SELECT 1"})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "/apis/query.grafana.app/v0alpha1/namespaces/default/query", capturedPath)
	assert.Equal(t, "application/json", capturedContentType)

	// Verify the query body carries the BigQuery plugin type, rawSql, and format.
	queries, ok := capturedBody["queries"].([]any)
	require.True(t, ok)
	require.Len(t, queries, 1)
	q, ok := queries[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "SELECT 1", q["rawSql"])
	assert.InDelta(t, 1, q["format"], 1e-9)
	ds, ok := q["datasource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "grafana-bigquery-datasource", ds["type"])
	assert.Equal(t, "bq-uid", ds["uid"])
}

func TestQuery_ReturnsTypedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"Syntax error: Unexpected keyword","errorSource":"downstream","status":400}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "bq-uid", bigquery.QueryRequest{RawSQL: "SELEC 1"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "bigquery", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "Syntax error")
}

func TestQuery_FallsBackOnNon200(t *testing.T) {
	callCount := 0
	var capturedPaths []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		capturedPaths = append(capturedPaths, r.URL.Path)
		if callCount == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[42]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	resp, err := client.Query(context.Background(), "bq-uid", bigquery.QueryRequest{RawSQL: "SELECT 42"})
	require.NoError(t, err)
	assert.Len(t, resp.Rows, 1)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "/apis/query.grafana.app/v0alpha1/namespaces/default/query", capturedPaths[0])
	assert.Equal(t, "/api/ds/query", capturedPaths[1])
}
