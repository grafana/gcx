package synth_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/query/synth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// newTestClient builds a synth.Client wired to a local httptest server.
func newTestClient(t *testing.T, handler http.Handler) *synth.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: server.URL}}
	client, err := synth.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestClient_ProxyGet(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/datasources/proxy/uid/sm-uid/sm/check/list", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"id":1}]`))
	}))

	body, err := client.ProxyGet(context.Background(), "sm-uid", "sm/check/list", "list checks")
	require.NoError(t, err)
	assert.Equal(t, `[{"id":1}]`, string(body))
}

func TestClient_ProxyGet_PropagatesError(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"plugin proxy route access denied"}`))
	}))

	_, err := client.ProxyGet(context.Background(), "sm-uid", "sm/probe/list", "list probes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin proxy route access denied")
}

func TestClient_ProxyPost(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/datasources/proxy/uid/sm-uid/sm/check/add", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"job":"my-job"}`, string(body))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))

	body, err := client.ProxyPost(context.Background(), "sm-uid", "sm/check/add", []byte(`{"job":"my-job"}`), "create check")
	require.NoError(t, err)
	assert.Equal(t, `{"id":42}`, string(body))
}

func TestClient_ProxyPost_AcceptsCreated(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))

	_, err := client.ProxyPost(context.Background(), "sm-uid", "sm/check/add", []byte(`{}`), "create check")
	require.NoError(t, err)
}

func TestClient_ProxyDelete(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/datasources/proxy/uid/sm-uid/sm/check/delete/7", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"msg":"check deleted"}`))
	}))

	body, err := client.ProxyDelete(context.Background(), "sm-uid", "sm/check/delete/7", "delete check")
	require.NoError(t, err)
	assert.Contains(t, string(body), "check deleted")
}

func TestClient_ProxyDelete_AcceptsNoContent(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	_, err := client.ProxyDelete(context.Background(), "sm-uid", "sm/probe/delete/1", "delete probe")
	require.NoError(t, err)
}
