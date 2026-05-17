package schemads_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/queryerror"
	"github.com/grafana/gcx/internal/schemads"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *schemads.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	c, err := schemads.NewClient(cfg)
	require.NoError(t, err)
	return c
}

func TestFullSchema_HappyPath(t *testing.T) {
	var gotPath, gotMethod string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"fullSchema": {
				"tables": [
					{"name": "up", "columns": [{"name": "timestamp", "type": "datetime"}, {"name": "value", "type": "float64"}]}
				],
				"capabilities": {"aggregateFunctions": ["SUM", "COUNT"]}
			}
		}`))
	}))

	schema, err := client.FullSchema(context.Background(), "abc123")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/datasources/uid/abc123/resources/abstractionSchema/fullSchema", gotPath)
	require.Len(t, schema.Tables, 1)
	assert.Equal(t, "up", schema.Tables[0].Name)
	require.NotNil(t, schema.Capabilities)
	assert.Equal(t, []string{"SUM", "COUNT"}, schema.Capabilities.AggregateFunctions)
}

func TestFullSchema_EscapesUID(t *testing.T) {
	var gotRawPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.RawPath
		if gotRawPath == "" {
			gotRawPath = r.URL.Path
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"fullSchema":{}}`))
	}))

	uid := "uid/../admin"
	_, err := client.FullSchema(context.Background(), uid)
	require.NoError(t, err)
	assert.Contains(t, gotRawPath, url.PathEscape(uid),
		"escaped uid must be preserved on the wire (RawPath=%q)", gotRawPath)
}

func TestFullSchema_ErrorBody(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"datasource not found"}`))
	}))

	_, err := client.FullSchema(context.Background(), "missing")
	require.Error(t, err)
	var apiErr *queryerror.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Contains(t, strings.ToLower(apiErr.Message), "datasource not found")
}

func TestColumns_HappyPath(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		_ = json.Unmarshal(buf[:n], &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"columns": {
				"up": [
					{"name": "timestamp", "type": "datetime"},
					{"name": "value", "type": "float64"},
					{"name": "instance", "type": "string", "operators": ["=", "in"]},
					{"name": "job", "type": "string", "operators": ["=", "in"]}
				]
			}
		}`))
	}))

	cr, err := client.Columns(context.Background(), "abc", []string{"up"}, nil)
	require.NoError(t, err)

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/datasources/uid/abc/resources/abstractionSchema/columns", gotPath)
	require.Equal(t, []any{"up"}, gotBody["tables"])

	require.Len(t, cr.Columns["up"], 4)
	assert.Equal(t, "instance", cr.Columns["up"][2].Name)
	assert.Equal(t, []schemads.Operator{"=", "in"}, cr.Columns["up"][2].Operators)
}

func TestColumns_EmptyTablesIsNoOp(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when tables is empty")
	}))
	cr, err := client.Columns(context.Background(), "abc", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, cr.Columns)
}

func TestColumns_TableMetadata(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"columns": {"prometheus_http_requests_total": [{"name":"timestamp","type":"datetime"}]},
			"tableMetadata": {
				"prometheus_http_requests_total": {
					"description": "Counter of HTTP requests.",
					"custom": {"prom.type": "counter"}
				}
			}
		}`))
	}))

	cr, err := client.Columns(context.Background(), "abc", []string{"prometheus_http_requests_total"}, nil)
	require.NoError(t, err)
	md := cr.TableMetadata["prometheus_http_requests_total"]
	assert.Equal(t, "Counter of HTTP requests.", md.Description)
	assert.Equal(t, "counter", md.Custom["prom.type"])
}

func TestFullSchema_NilFullSchemaIsEmpty(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	schema, err := client.FullSchema(context.Background(), "x")
	require.NoError(t, err)
	require.NotNil(t, schema)
	assert.Empty(t, schema.Tables)
}
