package clickhouse_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, srvURL string) *clickhouse.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srvURL},
		Namespace: "default",
	}
	client, err := clickhouse.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestQuery(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		assertResp func(t *testing.T, resp *clickhouse.QueryResponse)
	}{
		{
			name: "parses columnar response",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"col1","type":"string"},{"name":"col2","type":"number"}]},"data":{"values":[["a","b"],[1,2]]}}],"status":200}}}`))
			}),
			assertResp: func(t *testing.T, resp *clickhouse.QueryResponse) {
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
			assertResp: func(t *testing.T, resp *clickhouse.QueryResponse) {
				t.Helper()
				assert.Len(t, resp.Columns, 1)
				assert.Empty(t, resp.Rows)
			},
		},
		{
			name: "nullable values",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"name","type":"string"},{"name":"total_rows","type":"number"}]},"data":{"values":[["t1","t2"],[100,null]]}}],"status":200}}}`))
			}),
			assertResp: func(t *testing.T, resp *clickhouse.QueryResponse) {
				t.Helper()
				assert.Len(t, resp.Rows, 2)
				assert.Nil(t, resp.Rows[1][1])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			client := newTestClient(t, server.URL)
			resp, err := client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{RawSQL: "SELECT 1"})
			require.NoError(t, err)
			tt.assertResp(t, resp)
		})
	}
}

func TestQuery_RequestConstruction(t *testing.T) {
	var capturedPath string
	var capturedMethod string
	var capturedContentType string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{RawSQL: "SELECT 1"})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "/apis/query.grafana.app/v0alpha1/namespaces/default/query", capturedPath)
	assert.Equal(t, "application/json", capturedContentType)
}

func TestQuery_ReturnsTypedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"Code: 62. DB::Exception: Syntax error","errorSource":"downstream","status":400}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{RawSQL: "SELECT 1"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "clickhouse", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "Syntax error")
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
	resp, err := client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{RawSQL: "SELECT 42"})
	require.NoError(t, err)
	assert.Len(t, resp.Rows, 1)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "/apis/query.grafana.app/v0alpha1/namespaces/default/query", capturedPaths[0])
	assert.Equal(t, "/api/ds/query", capturedPaths[1])
}
