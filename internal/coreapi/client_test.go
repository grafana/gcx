package coreapi_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/coreapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, url string) *coreapi.Client {
	t.Helper()
	cfg := config.NamespacedRESTConfig{Config: rest.Config{Host: url}}
	c, err := coreapi.NewClient(cfg)
	require.NoError(t, err)
	return c
}

type widget struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func TestDoJSON_DecodesResponse(t *testing.T) {
	var gotMethod, gotPath, gotAccept, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":7,"name":"alpha"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	got, err := coreapi.DoJSON[any, widget](context.Background(), c, http.MethodGet, "/api/widgets/7", nil, http.StatusOK)
	require.NoError(t, err)
	assert.Equal(t, widget{ID: 7, Name: "alpha"}, got)
	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "/api/widgets/7", gotPath)
	assert.Equal(t, "application/json", gotAccept)
	assert.Empty(t, gotContentType, "GET with nil body must not set Content-Type")
}

func TestDoJSON_MarshalsBodyAndSetsContentType(t *testing.T) {
	var gotBody, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":9,"name":"beta"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	in := widget{Name: "beta"}
	got, err := coreapi.DoJSON[widget, widget](context.Background(), c, http.MethodPost, "/api/widgets", &in, http.StatusOK, http.StatusCreated)
	require.NoError(t, err)
	assert.Equal(t, widget{ID: 9, Name: "beta"}, got)
	assert.JSONEq(t, `{"id":0,"name":"beta"}`, gotBody)
	assert.Equal(t, "application/json", gotContentType)
}

func TestDoJSON_ErrorParsesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"permission denied"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	_, err := coreapi.DoJSON[any, widget](context.Background(), c, http.MethodGet, "/api/widgets/1", nil, http.StatusOK)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
	assert.Contains(t, err.Error(), "permission denied")
}

func TestDoJSONNotFound_ReturnsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	sentinel := errors.New("missing")
	c := newTestClient(t, srv.URL)
	_, err := coreapi.DoJSONNotFound[any, widget](context.Background(), c, http.MethodGet, "/api/widgets/1", nil, sentinel, http.StatusOK)
	require.ErrorIs(t, err, sentinel)
}

func TestDoStatus_ChecksStatusWithoutDecoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := coreapi.DoStatus[any](context.Background(), c, http.MethodDelete, "/api/widgets/1", nil, http.StatusOK, http.StatusNoContent)
	require.NoError(t, err)
}

func TestDoStatus_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`boom`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	err := coreapi.DoStatus[any](context.Background(), c, http.MethodDelete, "/api/widgets/1", nil, http.StatusOK)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, err.Error(), "boom")
}

func TestReadInput(t *testing.T) {
	t.Run("reads from stdin when path is -", func(t *testing.T) {
		got, err := coreapi.ReadInput("-", strings.NewReader(`{"a":1}`))
		require.NoError(t, err)
		assert.JSONEq(t, `{"a":1}`, string(got))
	})

	t.Run("reads from file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "spec.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"b":2}`), 0o600))
		got, err := coreapi.ReadInput(path, nil)
		require.NoError(t, err)
		assert.JSONEq(t, `{"b":2}`, string(got))
	})

	t.Run("errors on missing file", func(t *testing.T) {
		_, err := coreapi.ReadInput(filepath.Join(t.TempDir(), "nope.json"), nil)
		require.Error(t, err)
	})
}
