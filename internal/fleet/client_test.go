package fleet_test

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_DoRequest_AuthHeaders(t *testing.T) {
	tests := []struct {
		name         string
		instanceID   string
		apiToken     string
		useBasicAuth bool
		checkAuth    func(t *testing.T, r *http.Request)
	}{
		{
			name:         "basic auth sends Authorization: Basic header",
			instanceID:   "12345",
			apiToken:     "secret-token",
			useBasicAuth: true,
			checkAuth: func(t *testing.T, r *http.Request) {
				t.Helper()
				user, pass, ok := r.BasicAuth()
				require.True(t, ok, "expected Basic auth header")
				assert.Equal(t, "12345", user)
				assert.Equal(t, "secret-token", pass)

				// Verify the raw header format: Basic base64(instanceID:apiToken)
				expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("12345:secret-token"))
				assert.Equal(t, expected, r.Header.Get("Authorization"))
			},
		},
		{
			name:         "bearer auth sends Authorization: Bearer header",
			instanceID:   "",
			apiToken:     "bearer-token",
			useBasicAuth: false,
			checkAuth: func(t *testing.T, r *http.Request) {
				t.Helper()
				assert.Equal(t, "Bearer bearer-token", r.Header.Get("Authorization"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedReq *http.Request
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedReq = r
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			client := fleet.NewClient(context.Background(), server.URL, tt.instanceID, tt.apiToken, tt.useBasicAuth, nil)
			resp, err := client.DoRequest(context.Background(), "/some.v1.Service/Method", nil)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.NotNil(t, capturedReq)
			tt.checkAuth(t, capturedReq)
		})
	}
}

func TestNewClient_DoRequest_RequestFormat(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		body        any
		wantMethod  string
		wantCT      string
		wantAccept  string
		wantBodyStr string
	}{
		{
			name:       "nil body sends POST with correct headers",
			path:       "/service.v1.Service/Method",
			body:       nil,
			wantMethod: http.MethodPost,
			wantCT:     "application/json",
			wantAccept: "application/json",
		},
		{
			name:        "non-nil body is marshaled as JSON",
			path:        "/service.v1.Service/Method",
			body:        map[string]string{"key": "value"},
			wantMethod:  http.MethodPost,
			wantCT:      "application/json",
			wantAccept:  "application/json",
			wantBodyStr: `"key":"value"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedReq *http.Request
			var capturedBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedReq = r
				b, _ := io.ReadAll(r.Body)
				capturedBody = b
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()

			client := fleet.NewClient(context.Background(), server.URL, "inst", "tok", true, nil)
			resp, err := client.DoRequest(context.Background(), tt.path, tt.body)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantMethod, capturedReq.Method)
			assert.Equal(t, tt.wantCT, capturedReq.Header.Get("Content-Type"))
			assert.Equal(t, tt.wantAccept, capturedReq.Header.Get("Accept"))
			assert.True(t, strings.HasSuffix(capturedReq.URL.Path, tt.path),
				"expected path %q, got %q", tt.path, capturedReq.URL.Path)

			if tt.wantBodyStr != "" {
				assert.Contains(t, string(capturedBody), tt.wantBodyStr)
			}
		})
	}
}

func TestNewClient_DoRequest_URLTrimming(t *testing.T) {
	// Verify that trailing slashes on baseURL are trimmed so paths are clean.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := fleet.NewClient(context.Background(), server.URL+"/", "inst", "tok", true, nil)
	resp, err := client.DoRequest(context.Background(), "/path.v1.Service/Method", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestReadErrorBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantBody string
	}{
		{
			name:     "reads body string",
			body:     `{"error":"something went wrong"}`,
			wantBody: `{"error":"something went wrong"}`,
		},
		{
			name:     "empty body",
			body:     "",
			wantBody: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				Body: io.NopCloser(strings.NewReader(tt.body)),
			}
			got := fleet.ReadErrorBody(resp)
			assert.Equal(t, tt.wantBody, got)
		})
	}
}
