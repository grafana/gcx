package fleet_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const (
	// proxyFleetBase is the collector-app app plugin-proxy prefix the base client
	// targets for Fleet Management traffic.
	proxyFleetBase = "/api/plugin-proxy/grafana-collector-app/fleet-management-api"
	// proxyInstancesPath is the Viewer-role instance-metadata proxy route.
	proxyInstancesPath = "/api/plugin-proxy/grafana-collector-app/grafanacom-api/instances/"
)

// newTestClient builds a base client whose Grafana bearer credential is
// "test-bearer", pointed at the given host. The bearer is injected by the k8s
// round-tripper from rest.HTTPClientFor — never set by the client itself.
func newTestClient(t *testing.T, host string) *fleet.Client {
	t.Helper()
	c, err := fleet.NewClient(config.NamespacedRESTConfig{
		Config: rest.Config{Host: host, BearerToken: "test-bearer"},
	})
	require.NoError(t, err)
	return c
}

// TestNewClient_DoRequest_PluginProxyTransport asserts the base client targets
// the collector-app fleet-management-api proxy route, relies on the round-tripper
// for the Grafana bearer, and sets no Basic auth or client-side Authorization of
// its own (FR-001, FR-003, and the "no client-set Basic/Authorization" AC).
func TestNewClient_DoRequest_PluginProxyTransport(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	resp, err := client.DoRequest(context.Background(), "/pipeline.v1.PipelineService/ListPipelines", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.NotNil(t, captured)

	// URL is cfg.Host + collector-app fleet-management-api prefix + the Connect
	// service/method path, byte-for-byte.
	assert.Equal(t, proxyFleetBase+"/pipeline.v1.PipelineService/ListPipelines", captured.URL.Path)

	// The Grafana bearer is injected by the k8s round-tripper.
	assert.Equal(t, "Bearer test-bearer", captured.Header.Get("Authorization"))

	// The client sets no Basic auth of its own.
	_, _, hasBasic := captured.BasicAuth()
	assert.False(t, hasBasic, "client must not set Basic auth")

	// The client sets no X-Prom-*/X-Scope-OrgID headers (the proxy injects them).
	assert.Empty(t, captured.Header.Get("X-Prom-Cluster-Id"))
	assert.Empty(t, captured.Header.Get("X-Prom-Instance-Id"))
	assert.Empty(t, captured.Header.Get("X-Scope-Orgid"))
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

			client := newTestClient(t, server.URL)
			resp, err := client.DoRequest(context.Background(), tt.path, tt.body)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.wantMethod, capturedReq.Method)
			assert.Equal(t, tt.wantCT, capturedReq.Header.Get("Content-Type"))
			assert.Equal(t, tt.wantAccept, capturedReq.Header.Get("Accept"))
			// Connect service/method path suffix is preserved unchanged under the proxy prefix.
			assert.Equal(t, proxyFleetBase+tt.path, capturedReq.URL.Path)

			if tt.wantBodyStr != "" {
				assert.Contains(t, string(capturedBody), tt.wantBodyStr)
			}
		})
	}
}

// TestClient_FetchInstanceMetadata asserts the Viewer-role instance-metadata
// proxy route is a GET at the expected path and decodes into cloud.StackInfo,
// reusing the same GCOM /api/instances/{stackId} shape (FR-009, OQ-1).
func TestClient_FetchInstanceMetadata(t *testing.T) {
	var captured *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"hmInstancePromUrl": "https://prometheus-prod-1.grafana.net",
			"hmInstancePromId": 111,
			"hmInstancePromClusterId": 7,
			"hlInstanceUrl": "https://logs-prod-1.grafana.net",
			"hlInstanceId": 222,
			"htInstanceUrl": "https://tempo-prod-1.grafana.net",
			"htInstanceId": 333,
			"hpInstanceUrl": "https://profiles-prod-1.grafana.net",
			"hpInstanceId": 444,
			"agentManagementInstanceUrl": "https://fleet-management-prod-1.grafana.net",
			"agentManagementInstanceId": 555,
			"orgSlug": "myorg"
		}`))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	stack, err := client.FetchInstanceMetadata(context.Background())
	require.NoError(t, err)

	require.NotNil(t, captured)
	assert.Equal(t, http.MethodGet, captured.Method)
	assert.Equal(t, proxyInstancesPath, captured.URL.Path)
	assert.Equal(t, "Bearer test-bearer", captured.Header.Get("Authorization"))

	assert.Equal(t, "https://prometheus-prod-1.grafana.net", stack.HMInstancePromURL)
	assert.Equal(t, 111, stack.HMInstancePromID)
	assert.Equal(t, "https://fleet-management-prod-1.grafana.net", stack.AgentManagementInstanceURL)
	assert.Equal(t, "myorg", stack.OrgSlug)
}

func TestClient_FetchInstanceMetadata_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.FetchInstanceMetadata(context.Background())
	require.Error(t, err)
	var httpErr *fleet.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusForbidden, httpErr.Status)
}

// TestHTTPError_Error asserts Error() reports only the raw HTTP diagnostic.
// Role/auth guidance is added once by the cmd/gcx/fail converter (as
// DetailedError suggestions), not appended here, so it must be absent from the
// error string to avoid duplicate emission.
func TestHTTPError_Error(t *testing.T) {
	err := &fleet.HTTPError{Status: http.StatusForbidden, Path: "/x", Body: "boom"}
	msg := err.Error()
	assert.Equal(t, "fleet: HTTP 403 from /x: boom", msg)
	assert.NotContains(t, msg, "Grafana Admin")
	assert.NotContains(t, msg, "gcx login")
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
