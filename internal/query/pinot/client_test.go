package pinot_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/pinot"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, srvURL string) *pinot.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srvURL},
		Namespace: "default",
	}
	client, err := pinot.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestQuery(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		assertResp func(t *testing.T, resp *querysql.QueryResponse)
	}{
		{
			name: "parses columnar response",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"col1","type":"string"},{"name":"col2","type":"number"}]},"data":{"values":[["a","b"],[1,2]]}}],"status":200}}}`))
			}),
			assertResp: func(t *testing.T, resp *querysql.QueryResponse) {
				t.Helper()
				assert.Len(t, resp.Columns, 2)
				assert.Equal(t, "col1", resp.Columns[0].Name)
				assert.Equal(t, "col2", resp.Columns[1].Name)
				assert.Len(t, resp.Rows, 2)
				assert.Equal(t, "a", resp.Rows[0][0])
				assert.Equal(t, "b", resp.Rows[1][0])
			},
		},
		{
			name: "empty result",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"x","type":"string"}]},"data":{"values":[[]]}}],"status":200}}}`))
			}),
			assertResp: func(t *testing.T, resp *querysql.QueryResponse) {
				t.Helper()
				assert.Len(t, resp.Columns, 1)
				assert.Empty(t, resp.Rows)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server.URL)
			resp, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
				RawSQL: "SELECT 1",
			})
			require.NoError(t, err)
			tt.assertResp(t, resp)
		})
	}
}

func TestQuery_RequestConstruction(t *testing.T) {
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL: "SELECT count(*) FROM faro_pinot_events_v2",
	})
	require.NoError(t, err)

	queries, ok := captured["queries"].([]any)
	require.True(t, ok)
	require.Len(t, queries, 1)
	q, ok := queries[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "A", q["refId"])
	assert.Equal(t, "PinotQL", q["queryType"])
	assert.Equal(t, "Code", q["editorMode"])
	assert.Equal(t, "TABLE", q["displayType"])
	assert.Equal(t, "faro_pinot_events_v2", q["tableName"])
	assert.Equal(t, "SELECT count(*) FROM faro_pinot_events_v2", q["pinotQlCode"])
	ds, ok := q["datasource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, pinot.DatasourceType, ds["type"])
	assert.Equal(t, "pinot-uid", ds["uid"])
}

func TestQuery_EmptyTableNameWhenSQLHasNoFrom(t *testing.T) {
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &captured))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{RawSQL: "SELECT 1"})
	require.NoError(t, err)

	q := captured["queries"].([]any)[0].(map[string]any)
	assert.Equal(t, "", q["tableName"])
	assert.Equal(t, "SELECT 1", q["pinotQlCode"])
}

func TestQuery_ReturnsTypedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"SQLParsingError: ...","errorSource":"downstream","status":400}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL: "SELECT 1",
	})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "pinot", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "SQLParsingError")
	assert.Equal(t, "downstream", apiErr.ErrorSource)
}

func TestQuery_FallsBackOn404(t *testing.T) {
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
	resp, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL: "SELECT 42",
	})
	require.NoError(t, err)
	assert.Len(t, resp.Rows, 1)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "/apis/query.grafana.app/v0alpha1/namespaces/default/query", capturedPaths[0])
	assert.Equal(t, "/api/ds/query", capturedPaths[1])
}
