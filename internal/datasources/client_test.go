package datasources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/datasources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *datasources.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	client, err := datasources.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestList_ReturnsTypedAPIError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources", r.URL.Path)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"access denied"}`))
	}))

	_, err := client.List(context.Background())
	require.Error(t, err)

	var apiErr *datasources.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "list datasources", apiErr.Operation)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, "access denied", apiErr.Message)
}

func TestGetByUID_ReturnsTypedNotFoundError(t *testing.T) {
	assertNotFoundAPIError(t,
		"/api/datasources/uid/missing",
		func(client *datasources.Client) error {
			_, err := client.GetByUID(context.Background(), "missing")
			return err
		},
		"get datasource",
	)
}

func TestHealth_ReturnsResult(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/uid/prom-001/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"OK","message":"Successfully queried the Prometheus API."}`))
	}))

	result, err := client.Health(context.Background(), "prom-001")
	require.NoError(t, err)
	assert.Equal(t, "OK", result.Status)
	assert.Equal(t, "Successfully queried the Prometheus API.", result.Message)
}

func TestHealth_ReturnsErrorResultOnNon200(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/uid/bad-prom/health", r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"ERROR","message":"connection refused"}`))
	}))

	result, err := client.Health(context.Background(), "bad-prom")
	require.NoError(t, err)
	assert.Equal(t, "ERROR", result.Status)
	assert.Equal(t, "connection refused", result.Message)
}

func TestHealth_ReturnsTypedAPIError(t *testing.T) {
	assertNotFoundAPIError(t,
		"/api/datasources/uid/missing/health",
		func(client *datasources.Client) error {
			_, err := client.Health(context.Background(), "missing")
			return err
		},
		"health check datasource",
	)
}

func assertNotFoundAPIError(t *testing.T, expectedPath string, call func(*datasources.Client) error, expectedOp string) {
	t.Helper()
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, expectedPath, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Datasource not found"}`))
	}))

	err := call(client)
	require.Error(t, err)

	var apiErr *datasources.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, expectedOp, apiErr.Operation)
	assert.Equal(t, "missing", apiErr.Identifier)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "Datasource not found", apiErr.Message)
}
