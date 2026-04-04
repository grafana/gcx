package assistanthttp_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func newTestClient(t *testing.T, handler http.Handler) *assistanthttp.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.NamespacedRESTConfig{
		Config:    rest.Config{Host: server.URL},
		Namespace: "default",
	}
	client, err := assistanthttp.NewClient(cfg)
	require.NoError(t, err)
	return client
}

func TestDoRequest_PrependsPluginBasePath(t *testing.T) {
	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	resp, err := client.DoRequest(context.Background(), http.MethodGet, "/investigations/summary", nil)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Contains(t, gotPath, "/api/plugins/grafana-assistant-app/resources/api/v1/investigations/summary")
}

func TestDoRequest_SetsContentTypeForPOST(t *testing.T) {
	var gotContentType string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"title":"test"}`)
	resp, err := client.DoRequest(context.Background(), http.MethodPost, "/investigations", body)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "application/json", gotContentType)
}

func TestDoRequest_NoContentTypeForGET(t *testing.T) {
	var gotContentType string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))

	resp, err := client.DoRequest(context.Background(), http.MethodGet, "/investigations/summary", nil)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Empty(t, gotContentType)
}

func TestHandleErrorResponse_WithBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Body:       io.NopCloser(strings.NewReader("investigation not found")),
	}
	err := assistanthttp.HandleErrorResponse(resp)
	assert.EqualError(t, err, "request failed with status 404: investigation not found")
}

func TestHandleErrorResponse_EmptyBody(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("")),
	}
	err := assistanthttp.HandleErrorResponse(resp)
	assert.EqualError(t, err, "request failed with status 500")
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name string
		time time.Time
		want string
	}{
		{name: "zero", time: time.Time{}, want: "-"},
		{name: "valid", time: time.Date(2026, 4, 1, 14, 30, 0, 0, time.UTC), want: "2026-04-01 14:30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, assistanthttp.FormatTime(tt.time))
		})
	}
}
