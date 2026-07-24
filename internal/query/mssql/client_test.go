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
