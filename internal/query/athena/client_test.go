package athena_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/athena"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, srvURL string) *athena.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srvURL},
		Namespace: "default",
	}
	c, err := athena.NewClient(cfg)
	require.NoError(t, err)
	return c
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
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"id","type":"number"},{"name":"name","type":"string"}]},"data":{"values":[[1,2],["alice","bob"]]}}]}}}`))
			}),
			assertResp: func(t *testing.T, resp *querysql.QueryResponse) {
				t.Helper()
				assert.Len(t, resp.Columns, 2)
				assert.Equal(t, "id", resp.Columns[0].Name)
				assert.Equal(t, "name", resp.Columns[1].Name)
				assert.Len(t, resp.Rows, 2)
				assert.Equal(t, "alice", resp.Rows[0][1])
				assert.Equal(t, "bob", resp.Rows[1][1])
			},
		},
		{
			name: "empty result",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"x","type":"string"}]},"data":{"values":[[]]}}]}}}`))
			}),
			assertResp: func(t *testing.T, resp *querysql.QueryResponse) {
				t.Helper()
				assert.Len(t, resp.Columns, 1)
				assert.Empty(t, resp.Rows)
			},
		},
		{
			name: "nullable values",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"name","type":"string"},{"name":"count","type":"number"}]},"data":{"values":[["t1","t2"],[100,null]]}}]}}}`))
			}),
			assertResp: func(t *testing.T, resp *querysql.QueryResponse) {
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
			resp, err := client.Query(context.Background(), "test-uid", athena.QueryRequest{
				RawSQL: "SELECT 1",
				Start:  time.Now().Add(-1 * time.Hour),
				End:    time.Now(),
			})
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
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}]}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "test-uid", athena.QueryRequest{
		RawSQL: "SELECT 1",
		Start:  time.Now().Add(-1 * time.Hour),
		End:    time.Now(),
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "/apis/query.grafana.app/v0alpha1/namespaces/default/query", capturedPath)
	assert.Equal(t, "application/json", capturedContentType)
}

func TestQuery_ReturnsTypedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"SYNTAX_ERROR: line 1:8: mismatched input","errorSource":"downstream","status":400}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "test-uid", athena.QueryRequest{
		RawSQL: "SELECT ???",
		Start:  time.Now().Add(-1 * time.Hour),
		End:    time.Now(),
	})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "athena", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "SYNTAX_ERROR")
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
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"n","type":"number"}]},"data":{"values":[[42]]}}]}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	resp, err := client.Query(context.Background(), "test-uid", athena.QueryRequest{
		RawSQL: "SELECT 42",
		Start:  time.Now().Add(-1 * time.Hour),
		End:    time.Now(),
	})
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "expected 2 HTTP calls (K8s + fallback)")
	assert.Equal(t, "/apis/query.grafana.app/v0alpha1/namespaces/default/query", capturedPaths[0])
	assert.Equal(t, "/api/ds/query", capturedPaths[1])
	require.Len(t, resp.Rows, 1)
	assert.InDelta(t, float64(42), resp.Rows[0][0], 0.001)
}

func TestResource(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/datasources/uid/test-uid/resources/catalogs", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`["AwsDataCatalog","IcebergCatalog"]`))
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	c := newTestClient(t, server.URL)
	data, err := c.Resource(context.Background(), "test-uid", "/catalogs", map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, `["AwsDataCatalog","IcebergCatalog"]`, string(data))
}
