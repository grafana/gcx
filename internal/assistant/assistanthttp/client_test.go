package assistanthttp_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/assistant/assistanthttp"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
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

	resp, err := client.DoRequest(context.Background(), http.MethodGet, "/api/v1/investigations/summary", nil)
	require.NoError(t, err)
	resp.Body.Close()

	assert.Equal(t, "/api/plugins/grafana-assistant-app/resources/api/v1/investigations/summary", gotPath)
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

func TestHandleErrorResponse(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"plain text", http.StatusNotFound, "investigation not found", "request failed with status 404: investigation not found"},
		{"empty body", http.StatusInternalServerError, "", "request failed with status 500"},
		{"json remains raw", http.StatusForbidden, `{"message":"forbidden","traceID":"abc123"}`, `request failed with status 403: {"message":"forbidden","traceID":"abc123"}`},
		{"HTML remains raw", http.StatusUnauthorized, "<html>login</html>", "request failed with status 401: <html>login</html>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.status,
				Body:       io.NopCloser(strings.NewReader(tt.body)),
			}
			err := assistanthttp.HandleErrorResponse(resp)
			require.EqualError(t, err, tt.want)
			var statusErr *gcxerrors.HTTPStatusError
			require.ErrorAs(t, err, &statusErr)
			assert.Equal(t, tt.status, statusErr.HTTPStatusCode())
			assert.Equal(t, tt.body, statusErr.ServerMessage)
			assert.Empty(t, statusErr.TraceID, "Assistant preserves the raw body without parsing JSON")
			require.NoError(t, errors.Unwrap(err))
		})
	}
}

func TestHandleErrorResponse_ReadFailure(t *testing.T) {
	readErr := errors.New("body read failed")
	resp := &http.Response{
		StatusCode: http.StatusBadGateway,
		Body:       io.NopCloser(failingReader{err: readErr}),
	}
	err := assistanthttp.HandleErrorResponse(resp)
	require.EqualError(t, err, "request failed with status 502 (could not read body: body read failed)")
	require.ErrorIs(t, err, readErr)
	var statusErr *gcxerrors.HTTPStatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusBadGateway, statusErr.HTTPStatusCode())
	assert.Empty(t, statusErr.ServerMessage)
	assert.Empty(t, statusErr.TraceID)
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

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
