package datasources_test

import (
	"context"
	"errors"
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
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, "list datasources", apiErr.Operation)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, "access denied", apiErr.Message)
}

func TestGetByUID_ReturnsTypedNotFoundError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/uid/missing", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Datasource not found"}`))
	}))

	_, err := client.GetByUID(context.Background(), "missing")
	require.Error(t, err)

	var apiErr *datasources.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, "get datasource", apiErr.Operation)
	assert.Equal(t, "missing", apiErr.Identifier)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.Equal(t, "Datasource not found", apiErr.Message)
}
