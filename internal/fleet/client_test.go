package fleet_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The base client sets no credentials of its own. The caller supplies an
// *http.Client whose transport authenticates against the Grafana stack, and the
// collector app plugin proxy adds the Fleet Management credentials server-side.
func TestNewClient_DoRequest_SetsNoAuthHeader(t *testing.T) {
	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := fleet.NewClient(context.Background(), server.URL, nil)
	resp, err := client.DoRequest(context.Background(), "/some.v1.Service/Method", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.NotNil(t, capturedReq)
	assert.Empty(t, capturedReq.Header.Get("Authorization"))
}

// The transport of the supplied *http.Client carries the credential.
func TestNewClient_DoRequest_UsesSuppliedHTTPClient(t *testing.T) {
	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	httpClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		req.Header.Set("Authorization", "Bearer from-transport")
		return http.DefaultTransport.RoundTrip(req)
	})}

	client := fleet.NewClient(context.Background(), server.URL, httpClient)
	resp, err := client.DoRequest(context.Background(), "/some.v1.Service/Method", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.NotNil(t, capturedReq)
	assert.Equal(t, "Bearer from-transport", capturedReq.Header.Get("Authorization"))
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

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

			client := fleet.NewClient(context.Background(), server.URL, nil)
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

// The base URL carries the plugin proxy prefix, so the client must not add a
// second slash between the prefix and the RPC path.
func TestNewClient_DoRequest_URLTrimming(t *testing.T) {
	var capturedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := fleet.NewClient(context.Background(), server.URL+"/api/plugin-proxy/grafana-collector-app/fleet-management-api/", nil)
	resp, err := client.DoRequest(context.Background(), "/path.v1.Service/Method", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "/api/plugin-proxy/grafana-collector-app/fleet-management-api/path.v1.Service/Method", capturedPath)
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
