package mssql_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/mssql"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, srvURL string) *mssql.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srvURL},
		Namespace: "default",
	}
	client, err := mssql.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"n","type":"number"},{"name":"msg","type":"string"}]},"data":{"values":[[1,2],["a",null]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	resp, err := client.Query(context.Background(), "mssql-uid", mssql.QueryRequest{RawSQL: "SELECT 1"})
	require.NoError(t, err)
	require.Len(t, resp.Columns, 2)
	assert.Equal(t, "n", resp.Columns[0].Name)
	assert.Len(t, resp.Rows, 2)
	assert.Equal(t, "a", resp.Rows[0][1])
	assert.Nil(t, resp.Rows[1][1])
}

// TestQuery_SendsStringTableFormat guards the key MSSQL divergence: the core
// plugin requires format:"table" (string). An integer format code makes the
// plugin return HTTP 500.
func TestQuery_SendsStringTableFormat(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "mssql-uid", mssql.QueryRequest{RawSQL: "SELECT 1"})
	require.NoError(t, err)

	var captured struct {
		Queries []struct {
			Format     any `json:"format"`
			Datasource struct {
				Type string `json:"type"`
				UID  string `json:"uid"`
			} `json:"datasource"`
		} `json:"queries"`
	}
	require.NoError(t, json.Unmarshal(capturedBody, &captured))
	require.Len(t, captured.Queries, 1)
	assert.Equal(t, "table", captured.Queries[0].Format)
	assert.Equal(t, "mssql", captured.Queries[0].Datasource.Type)
	assert.Equal(t, "mssql-uid", captured.Queries[0].Datasource.UID)
}

// TestQuery_IntervalMs verifies --step is forwarded as intervalMs when set,
// and defaults to 60000 when zero, matching mysql/postgres via the shared
// querysql.BuildRawQueryBody. An earlier version of this client omitted
// intervalMs entirely when zero, on the theory that the plugin would apply a
// sensible default for the $__interval macro; live testing against a real
// MSSQL datasource showed the opposite — an absent intervalMs resolves
// $__interval to a near-zero width, putting every row in its own bucket
// instead of aggregating.
func TestQuery_IntervalMs(t *testing.T) {
	capture := func(t *testing.T, req mssql.QueryRequest) map[string]any {
		t.Helper()
		var body []byte
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
		}))
		defer server.Close()

		_, err := newTestClient(t, server.URL).Query(context.Background(), "mssql-uid", req)
		require.NoError(t, err)

		var parsed struct {
			Queries []map[string]any `json:"queries"`
		}
		require.NoError(t, json.Unmarshal(body, &parsed))
		require.Len(t, parsed.Queries, 1)
		return parsed.Queries[0]
	}

	t.Run("set forwards intervalMs", func(t *testing.T) {
		q := capture(t, mssql.QueryRequest{RawSQL: "SELECT 1", IntervalMs: 3600000})
		assert.EqualValues(t, 3600000, q["intervalMs"])
	})

	t.Run("zero defaults intervalMs to 60000", func(t *testing.T) {
		q := capture(t, mssql.QueryRequest{RawSQL: "SELECT 1"})
		assert.EqualValues(t, 60000, q["intervalMs"])
	})
}

func TestQuery_ReturnsTypedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"Incorrect syntax near 'FRM'","errorSource":"downstream","status":400}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "mssql-uid", mssql.QueryRequest{RawSQL: "SELECT 1"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "mssql", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "Incorrect syntax")
}
