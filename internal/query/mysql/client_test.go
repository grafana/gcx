package mysql_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/mysql"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *mysql.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: srv.URL},
		Namespace: "default",
	}
	client, err := mysql.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func testQueryReq() mysql.QueryRequest {
	return mysql.QueryRequest{
		RawSQL: "SELECT status, count(*) FROM orders GROUP BY status",
		Start:  time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
		End:    time.Date(2026, 7, 14, 1, 0, 0, 0, time.UTC),
	}
}

func TestQuery(t *testing.T) {
	t.Run("parses rows", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/apis/query.grafana.app/v0alpha1/namespaces/default/query")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"status","type":"string"},{"name":"n","type":"number"}]},"data":{"values":[["pending","shipped"],[66,67]]}}]}}}`))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		require.Len(t, resp.Columns, 2)
		assert.Equal(t, "status", resp.Columns[0].Name)
		require.Len(t, resp.Rows, 2)
		assert.Equal(t, "pending", resp.Rows[0][0])
	})

	// Pins the wire shape: core SQL datasources expect rawSql plus a string
	// format ("table"), unlike sqlds-based plugins such as ClickHouse where
	// format is numeric.
	t.Run("request body shape", func(t *testing.T) {
		var (
			captured   map[string]any
			decodedErr error
		)
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decodedErr = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[]}}}`))
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		require.NoError(t, decodedErr)

		queries, ok := captured["queries"].([]any)
		require.True(t, ok)
		require.Len(t, queries, 1)
		q, ok := queries[0].(map[string]any)
		require.True(t, ok)

		assert.Equal(t, "SELECT status, count(*) FROM orders GROUP BY status", q["rawSql"])
		format, ok := q["format"].(string)
		require.True(t, ok, "format must be a JSON string, got %T", q["format"])
		assert.Equal(t, "table", format)

		ds, ok := q["datasource"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "mysql", ds["type"])
	})

	t.Run("error envelope returns typed API error", func(t *testing.T) {
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"error":"db query error: Error 1146 (42S02): Table 'testdb.x' doesn't exist","status":400}}}`))
		}))

		_, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.Error(t, err)

		var apiErr *queryerror.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "mysql", apiErr.Datasource)
		assert.Equal(t, 400, apiErr.StatusCode)
		assert.Contains(t, apiErr.Message, "42S02")
	})

	t.Run("K8s fallback to legacy /api/ds/query", func(t *testing.T) {
		var paths []string
		client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			if r.URL.Path != "/api/ds/query" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"one","type":"number"}]},"data":{"values":[[1]]}}]}}}`))
		}))

		resp, err := client.Query(context.Background(), "test-uid", testQueryReq())
		require.NoError(t, err)
		assert.Len(t, resp.Rows, 1)
		assert.Contains(t, paths, "/api/ds/query")
	})
}
