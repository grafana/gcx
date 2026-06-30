package infinity_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/infinity"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *infinity.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := infinity.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestQuery_SuccessfulQuery(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedContentType string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path
		capturedContentType = r.Header.Get("Content-Type")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"name","type":"string"},{"name":"value","type":"number"}]},"data":{"values":[["Alice","Bob"],[10,20]]}}]}}}`))
	})

	resp, err := client.Query(context.Background(), "test-ds-uid", infinity.QueryRequest{
		Expr: "$.data[*]",
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Contains(t, capturedPath, "/apis/query.grafana.app/v0alpha1/namespaces/default/query")
	assert.Equal(t, "application/json", capturedContentType)

	require.NotNil(t, resp)
	assert.Equal(t, []infinity.Column{
		{Name: "name", Type: "string"},
		{Name: "value", Type: "number"},
	}, resp.Columns)

	require.Len(t, resp.Rows, 2)
	assert.Equal(t, []any{"Alice", float64(10)}, resp.Rows[0])
	assert.Equal(t, []any{"Bob", float64(20)}, resp.Rows[1])
}

func TestQuery_RequestBodyShape(t *testing.T) {
	var capturedBody map[string]any

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		err := json.NewDecoder(r.Body).Decode(&capturedBody)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"x","type":"string"}]},"data":{"values":[["v"]]}}]}}}`))
	})

	_, err := client.Query(context.Background(), "test-ds-uid", infinity.QueryRequest{
		Expr: "$.data[*]",
	})
	require.NoError(t, err)

	queries, ok := capturedBody["queries"].([]any)
	require.True(t, ok, "queries should be an array")
	require.Len(t, queries, 1)

	q, ok := queries[0].(map[string]any)
	require.True(t, ok, "query should be a map")

	ds, ok := q["datasource"].(map[string]any)
	require.True(t, ok, "datasource should be a map")
	assert.Equal(t, "yesoreyeram-infinity-datasource", ds["type"])
	assert.Equal(t, "test-ds-uid", ds["uid"])

	assert.Equal(t, "table", q["format"])
	assert.Equal(t, "backend", q["parser"])
	assert.Equal(t, "$.data[*]", q["root_selector"])
	assert.Equal(t, "A", q["refId"])
	assert.Equal(t, "url", q["source"])
}

func TestQuery_TimeRange(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		wantFrom string
		wantTo   string
	}{
		{
			name:     "with time range",
			start:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
			wantFrom: strconv.FormatInt(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), 10),
			wantTo:   strconv.FormatInt(time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC).UnixMilli(), 10),
		},
		{
			name:     "without time range",
			start:    time.Time{},
			end:      time.Time{},
			wantFrom: "now-1h",
			wantTo:   "now",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody map[string]any

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				err := json.NewDecoder(r.Body).Decode(&capturedBody)
				assert.NoError(t, err)

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"x","type":"string"}]},"data":{"values":[["v"]]}}]}}}`))
			})

			_, err := client.Query(context.Background(), "ds-uid", infinity.QueryRequest{
				Expr:  "$.data[*]",
				Start: tt.start,
				End:   tt.end,
			})
			require.NoError(t, err)

			// JSON numbers decode as float64 by default; string values stay as strings.
			// The "from" and "to" fields are either epoch-millis strings or relative strings.
			gotFrom, ok := capturedBody["from"].(string)
			require.True(t, ok, "from should be a string, got %T", capturedBody["from"])
			gotTo, ok := capturedBody["to"].(string)
			require.True(t, ok, "to should be a string, got %T", capturedBody["to"])

			assert.Equal(t, tt.wantFrom, gotFrom)
			assert.Equal(t, tt.wantTo, gotTo)
		})
	}
}

func TestQuery_GrafanaEnvelopeError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"invalid expression","errorSource":"downstream","status":400}}}`))
	})

	_, err := client.Query(context.Background(), "ds-uid", infinity.QueryRequest{Expr: "$.bad"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "infinity", apiErr.Datasource)
	assert.Equal(t, "query", apiErr.Operation)
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Equal(t, "downstream", apiErr.ErrorSource)
}

func TestQuery_GrafanaEnvelopeErrorStatusZeroFallback(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"A":{"error":"unknown field","errorSource":"downstream"}}}`))
	})

	_, err := client.Query(context.Background(), "ds-uid", infinity.QueryRequest{Expr: "$.x"})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 400, apiErr.StatusCode, "status should fall back to 400 when envelope status is 0")
}

func TestQuery_HTTPErrorReturnsAPIError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{name: "unauthorized", statusCode: 401},
		{name: "internal server error", statusCode: 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"message":"error text"}`))
			})

			_, err := client.Query(context.Background(), "ds-uid", infinity.QueryRequest{Expr: "$.x"})
			require.Error(t, err)

			var apiErr *queryerror.APIError
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, "infinity", apiErr.Datasource)
			assert.Equal(t, tt.statusCode, apiErr.StatusCode)
		})
	}
}

// Any non-200 from the K8s query API (404 not deployed, 403 RBAC, 500 from a
// rolling-out query service, ...) should fall back to /api/ds/query.
func TestQuery_FallbackOnNon200(t *testing.T) {
	statuses := []struct {
		name        string
		firstStatus int
	}{
		{"not found", http.StatusNotFound},
		{"forbidden", http.StatusForbidden},
		{"server error", http.StatusInternalServerError},
	}

	for _, tc := range statuses {
		t.Run(tc.name, func(t *testing.T) {
			var callCount atomic.Int32
			var mu sync.Mutex
			var paths []string

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				n := callCount.Add(1)

				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()

				if n == 1 {
					w.WriteHeader(tc.firstStatus)
					_, _ = w.Write([]byte(`{"message":"error"}`))
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"id","type":"number"}]},"data":{"values":[[1]]}}]}}}`))
			})

			resp, err := client.Query(context.Background(), "ds-uid", infinity.QueryRequest{Expr: "$.data[*]"})
			require.NoError(t, err)

			assert.Equal(t, int32(2), callCount.Load(), "expected exactly 2 requests (primary + fallback)")

			mu.Lock()
			capturedPaths := make([]string, len(paths))
			copy(capturedPaths, paths)
			mu.Unlock()

			assert.Contains(t, capturedPaths[0], "/apis/query.grafana.app/v0alpha1")
			assert.Equal(t, "/api/ds/query", capturedPaths[1])

			require.NotNil(t, resp)
			assert.Equal(t, []infinity.Column{{Name: "id", Type: "number"}}, resp.Columns)
			require.Len(t, resp.Rows, 1)
			assert.Equal(t, []any{float64(1)}, resp.Rows[0])
		})
	}
}

func TestQuery_FallbackOn404BothFail(t *testing.T) {
	var callCount atomic.Int32

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})

	_, err := client.Query(context.Background(), "ds-uid", infinity.QueryRequest{Expr: "$.x"})
	require.Error(t, err)

	assert.Equal(t, int32(2), callCount.Load(), "expected exactly 2 requests (primary + fallback)")

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
}

func TestQuery_ResponseBodyTooLarge(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		chunk := make([]byte, 1024*1024) // 1 MB
		for range 51 {
			_, _ = w.Write(chunk)
		}
	})

	_, err := client.Query(context.Background(), "ds-uid", infinity.QueryRequest{Expr: "$.x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "50 MB")

	var apiErr *queryerror.APIError
	assert.NotErrorAs(t, err, &apiErr)
}
