package pinot_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

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
				RawSQL:    "SELECT 1",
				TableName: "events",
			})
			require.NoError(t, err)
			tt.assertResp(t, resp)
		})
	}
}

func TestQuery_RequestConstruction(t *testing.T) {
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL: "SELECT count(*) FROM events",
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
	assert.Equal(t, "events", q["tableName"])
	assert.Equal(t, "SELECT count(*) FROM events", q["pinotQlCode"])
	ds, ok := q["datasource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, pinot.DatasourceType, ds["type"])
	assert.Equal(t, "pinot-uid", ds["uid"])
}

func TestQuery_RequiresTableNameWhenSQLHasNoFrom(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("query must not be sent when tableName cannot be derived")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{RawSQL: "SELECT 1"})
	require.ErrorIs(t, err, pinot.ErrTableNameRequired)
	assert.Contains(t, err.Error(), "--table")
}

func TestQuery_ExplicitTableNameOverridesExtract(t *testing.T) {
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL:    "SELECT 1 FROM (SELECT 1) journey",
		TableName: "events",
	})
	require.NoError(t, err)

	queries, ok := captured["queries"].([]any)
	require.True(t, ok)
	require.Len(t, queries, 1)
	q, ok := queries[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "events", q["tableName"])
	assert.Equal(t, "SELECT 1 FROM (SELECT 1) journey", q["pinotQlCode"])
}

func TestQuery_DefaultsTimeRangeWhenBothUnset(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL:    "SELECT 1",
		TableName: "events",
	})
	require.NoError(t, err)

	fromStr, ok := captured["from"].(string)
	require.True(t, ok)
	from, err := strconv.ParseInt(fromStr, 10, 64)
	require.NoError(t, err)
	toStr, ok := captured["to"].(string)
	require.True(t, ok)
	to, err := strconv.ParseInt(toStr, 10, 64)
	require.NoError(t, err)
	assert.Greater(t, to, from)
	assert.InDelta(t, float64(time.Hour.Milliseconds()), float64(to-from), float64(5*time.Second.Milliseconds()))
}

func TestQuery_SendsExplicitTimeRange(t *testing.T) {
	start := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 8, 13, 0, 0, 0, time.UTC)

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &captured)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL:    "SELECT 1",
		TableName: "events",
		Start:     start,
		End:       end,
	})
	require.NoError(t, err)
	assert.Equal(t, strconv.FormatInt(start.UnixMilli(), 10), captured["from"])
	assert.Equal(t, strconv.FormatInt(end.UnixMilli(), 10), captured["to"])
}

func TestQuery_RejectsPartialTimeRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("query must not be sent when time range is partial")
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	start := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

	_, err := client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL:    "SELECT 1",
		TableName: "events",
		Start:     start,
	})
	require.ErrorIs(t, err, pinot.ErrPartialTimeRange)

	_, err = client.Query(context.Background(), "pinot-uid", pinot.QueryRequest{
		RawSQL:    "SELECT 1",
		TableName: "events",
		End:       start,
	})
	require.ErrorIs(t, err, pinot.ErrPartialTimeRange)
}

func TestQuery_TableNameExtraction(t *testing.T) {
	tests := []struct {
		name      string
		request   pinot.QueryRequest
		wantTable string
	}{
		{
			name:      "ignores FROM inside comment",
			request:   pinot.QueryRequest{RawSQL: "SELECT 1 -- FROM events\nFROM t"},
			wantTable: "t",
		},
		{
			name:      "subquery extracts inner table",
			request:   pinot.QueryRequest{RawSQL: "SELECT count(*) FROM (SELECT col FROM events) sub"},
			wantTable: "events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]any

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &captured)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"results":{"A":{"frames":[{"schema":{"fields":[{"name":"v","type":"number"}]},"data":{"values":[[1]]}}],"status":200}}}`))
			}))
			defer server.Close()

			client := newTestClient(t, server.URL)
			_, err := client.Query(context.Background(), "pinot-uid", tt.request)
			require.NoError(t, err)

			queries, ok := captured["queries"].([]any)
			require.True(t, ok)
			require.Len(t, queries, 1)
			q, ok := queries[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tt.wantTable, q["tableName"])
		})
	}
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
		RawSQL:    "SELECT 1",
		TableName: "events",
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
		RawSQL:    "SELECT 42",
		TableName: "events",
	})
	require.NoError(t, err)
	assert.Len(t, resp.Rows, 1)
	assert.Equal(t, 2, callCount)
	assert.Equal(t, "/apis/query.grafana.app/v0alpha1/namespaces/default/query", capturedPaths[0])
	assert.Equal(t, "/api/ds/query", capturedPaths[1])
}
