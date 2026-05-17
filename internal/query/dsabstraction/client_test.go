package dsabstraction_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/dsabstraction"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, namespace string, handler http.Handler) *dsabstraction.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: namespace,
	}
	client, err := dsabstraction.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestQuery_HappyPath(t *testing.T) {
	var gotPath, gotMethod, gotContentType string
	var gotBody map[string]any

	client := newTestClient(t, "stacks-1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"schema": {
				"name": "response",
				"meta": {"typeVersion": [0, 0], "custom": {"pushdownPlan": [{"handler": "engine", "node": "Project", "pushed": false, "reason": "column projection"}]}},
				"fields": [
					{"name": "a", "type": "number", "typeInfo": {"frame": "int8", "nullable": true}},
					{"name": "b", "type": "string", "typeInfo": {"frame": "string", "nullable": true}}
				]
			},
			"data": {"values": [[1, 2], ["x", "y"]]}
		}`))
	}))

	pushdown := false
	resp, err := client.Query(context.Background(), dsabstraction.SQLRequest{
		SQL:      "SELECT a, b FROM t",
		From:     "now-5m",
		To:       "now",
		Pushdown: &pushdown,
	})
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/apis/dsabstraction.grafana.app/v1alpha1/namespaces/stacks-1/query", gotPath)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "SELECT a, b FROM t", gotBody["query"])
	assert.Equal(t, "now-5m", gotBody["from"])
	assert.Equal(t, "now", gotBody["to"])
	assert.Equal(t, false, gotBody["pushdown"])

	require.Len(t, resp.Schema.Fields, 2)
	assert.Equal(t, "a", resp.Schema.Fields[0].Name)
	assert.Equal(t, 2, resp.RowCount())

	plan, err := resp.ParsePushdownPlan()
	require.NoError(t, err)
	require.Len(t, plan, 1)
	assert.Equal(t, "engine", plan[0].Handler)
	assert.Equal(t, "Project", plan[0].Node)
}

func TestQuery_OmitsPushdownWhenNil(t *testing.T) {
	var gotBody map[string]any
	client := newTestClient(t, "stacks-1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema":{"fields":[]},"data":{"values":[]}}`))
	}))

	_, err := client.Query(context.Background(), dsabstraction.SQLRequest{
		SQL: "SELECT 1", From: "now-5m", To: "now",
	})
	require.NoError(t, err)
	_, present := gotBody["pushdown"]
	assert.False(t, present, "pushdown should be omitted when nil")
}

func TestQuery_BareErrorBody(t *testing.T) {
	client := newTestClient(t, "stacks-1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"query execution failed: syntax error at position 4 near 'NOT'"}`))
	}))

	_, err := client.Query(context.Background(), dsabstraction.SQLRequest{
		SQL: "NOT VALID", From: "now-5m", To: "now",
	})
	require.Error(t, err)

	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr, "want *queryerror.APIError, got %T", err)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "syntax error")
	assert.True(t, apiErr.IsParseError())
}

func TestQuery_RequiresSQL(t *testing.T) {
	client := newTestClient(t, "stacks-1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be hit when sql is empty")
	}))
	_, err := client.Query(context.Background(), dsabstraction.SQLRequest{From: "now-5m", To: "now"})
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "sql is required")
}

func TestQuery_PlumsCookie(t *testing.T) {
	var gotCookie string
	client := newTestClient(t, "stacks-1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema":{"fields":[]},"data":{"values":[]}}`))
	}))

	_, err := client.Query(context.Background(), dsabstraction.SQLRequest{
		SQL: "SELECT 1", From: "now-5m", To: "now",
		Cookie: "grafana_session=abc123",
	})
	require.NoError(t, err)
	assert.Equal(t, "grafana_session=abc123", gotCookie)
}

func TestQuery_OmitsCookieHeaderWhenEmpty(t *testing.T) {
	var hadCookie bool
	client := newTestClient(t, "stacks-1", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadCookie = r.Header["Cookie"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"schema":{"fields":[]},"data":{"values":[]}}`))
	}))

	_, err := client.Query(context.Background(), dsabstraction.SQLRequest{
		SQL: "SELECT 1", From: "now-5m", To: "now",
	})
	require.NoError(t, err)
	assert.False(t, hadCookie, "Cookie header should not be sent when empty")
}

func TestNamespace(t *testing.T) {
	c := newTestClient(t, "stacks-42", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	assert.Equal(t, "stacks-42", c.Namespace())
}
