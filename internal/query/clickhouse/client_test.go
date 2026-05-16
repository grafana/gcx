package clickhouse_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestQuery(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		assertResp func(t *testing.T, resp *clickhouse.QueryResponse)
		wantErr    string
	}{
		{
			name: "parses columnar response",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			name: "returns typed API error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results":{"A":{"error":"Code: 62. DB::Exception: Syntax error","errorSource":"downstream","status":400}}}`))
			}),
			wantErr: "Syntax error",
		},
		{
			name: "nullable values",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			cfg := config.NamespacedRESTConfig{
				Config:    rest.Config{Host: server.URL},
				Namespace: "default",
			}
			client, err := clickhouse.NewClient(cfg)
			require.NoError(t, err)

			resp, err := client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{RawSQL: "SELECT 1"})
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.assertResp(t, resp)
		})
	}
}

func TestQuery_FallsBackOn404(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[42]]}}],"status":200}}}`))
	}))
	defer server.Close()

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}, Namespace: "default"}
	client, err := clickhouse.NewClient(cfg)
	require.NoError(t, err)

	resp, err := client.Query(context.Background(), "ch-uid", clickhouse.QueryRequest{RawSQL: "SELECT 42"})
	require.NoError(t, err)
	assert.Len(t, resp.Rows, 1)
	assert.Equal(t, 2, callCount)
}
