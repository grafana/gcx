package instrumentation_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient creates an instrumentation Client pointed at the given test server URL.
func newTestClient(serverURL string) *instrumentation.Client {
	f := fleet.NewClient(serverURL, "inst-id", "api-token", true, nil)
	return instrumentation.NewClient(f)
}

// captureHandler returns an http.HandlerFunc that captures the request and writes
// the provided JSON response body with the given status code.
func captureHandler(t *testing.T, statusCode int, respBody string) (http.HandlerFunc, *capturedRequest) {
	t.Helper()
	cr := &capturedRequest{}
	return func(w http.ResponseWriter, r *http.Request) {
		cr.Method = r.Method
		cr.Path = r.URL.Path
		cr.ContentType = r.Header.Get("Content-Type")
		cr.Accept = r.Header.Get("Accept")
		b, _ := io.ReadAll(r.Body)
		cr.Body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(respBody))
	}, cr
}

type capturedRequest struct {
	Method      string
	Path        string
	ContentType string
	Accept      string
	Body        string
}

func assertConnectRequest(t *testing.T, cr *capturedRequest, wantPath string) {
	t.Helper()
	assert.Equal(t, http.MethodPost, cr.Method, "must use POST")
	assert.Equal(t, "application/json", cr.ContentType, "must set Content-Type: application/json")
	assert.Equal(t, "application/json", cr.Accept, "must set Accept: application/json")
	assert.True(t, strings.HasSuffix(cr.Path, wantPath), "expected path suffix %q, got %q", wantPath, cr.Path)
}

func TestClient_GetAppInstrumentation(t *testing.T) { //nolint:dupl // Intentionally similar to RunK8sDiscovery — distinct endpoints.
	tests := []struct {
		name        string
		clusterName string
		respStatus  int
		respBody    string
		wantErr     bool
		wantNSCount int
	}{
		{
			name:        "returns namespaces on 200",
			clusterName: "prod-1",
			respStatus:  http.StatusOK,
			respBody:    `{"namespaces":[{"name":"default","selection":"included","tracing":true}]}`,
			wantNSCount: 1,
		},
		{
			name:        "empty namespaces on 200",
			clusterName: "empty-cluster",
			respStatus:  http.StatusOK,
			respBody:    `{}`,
			wantNSCount: 0,
		},
		{
			name:        "HTTP error returns error",
			clusterName: "bad-cluster",
			respStatus:  http.StatusInternalServerError,
			respBody:    `{"error":"internal"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cr := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(srv.URL)
			resp, err := client.GetAppInstrumentation(context.Background(), tt.clusterName)

			assertConnectRequest(t, cr, "/instrumentation.v1.InstrumentationService/GetAppInstrumentation")
			assert.Contains(t, cr.Body, tt.clusterName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, resp.Namespaces, tt.wantNSCount)
		})
	}
}

func TestClient_SetAppInstrumentation(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		namespaces  []instrumentation.NamespaceConfig
		respStatus  int
		respBody    string
		wantErr     bool
	}{
		{
			name:        "successful set",
			clusterName: "prod-1",
			namespaces: []instrumentation.NamespaceConfig{
				{Name: "default", Selection: "included", Tracing: true},
			},
			respStatus: http.StatusOK,
			respBody:   `{}`,
		},
		{
			name:        "HTTP error returns error",
			clusterName: "bad-cluster",
			respStatus:  http.StatusBadRequest,
			respBody:    `{"error":"bad request"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cr := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(srv.URL)
			err := client.SetAppInstrumentation(context.Background(), tt.clusterName, tt.namespaces)

			assertConnectRequest(t, cr, "/instrumentation.v1.InstrumentationService/SetAppInstrumentation")
			assert.Contains(t, cr.Body, tt.clusterName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_GetK8SInstrumentation(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		respStatus  int
		respBody    string
		wantErr     bool
		wantCost    bool
	}{
		{
			name:        "returns k8s config on 200",
			clusterName: "prod-1",
			respStatus:  http.StatusOK,
			respBody:    `{"costmetrics":true,"clusterevents":false}`,
			wantCost:    true,
		},
		{
			name:        "HTTP error returns error",
			clusterName: "bad",
			respStatus:  http.StatusNotFound,
			respBody:    `{}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cr := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(srv.URL)
			resp, err := client.GetK8SInstrumentation(context.Background(), tt.clusterName)

			assertConnectRequest(t, cr, "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation")
			assert.Contains(t, cr.Body, tt.clusterName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCost, resp.CostMetrics)
		})
	}
}

func TestClient_SetK8SInstrumentation(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		k8s         instrumentation.K8sSpec
		respStatus  int
		respBody    string
		wantErr     bool
	}{
		{
			name:        "successful set",
			clusterName: "prod-1",
			k8s:         instrumentation.K8sSpec{CostMetrics: true, NodeLogs: true},
			respStatus:  http.StatusOK,
			respBody:    `{}`,
		},
		{
			name:        "HTTP error returns error",
			clusterName: "bad",
			respStatus:  http.StatusInternalServerError,
			respBody:    `{"error":"server error"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cr := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(srv.URL)
			err := client.SetK8SInstrumentation(context.Background(), tt.clusterName, tt.k8s)

			assertConnectRequest(t, cr, "/instrumentation.v1.InstrumentationService/SetK8SInstrumentation")
			assert.Contains(t, cr.Body, tt.clusterName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_SetupK8sDiscovery(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		respStatus  int
		respBody    string
		wantErr     bool
	}{
		{
			name:        "successful setup",
			clusterName: "prod-1",
			respStatus:  http.StatusOK,
			respBody:    `{}`,
		},
		{
			name:        "HTTP error returns error",
			clusterName: "bad",
			respStatus:  http.StatusForbidden,
			respBody:    `{"error":"forbidden"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cr := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(srv.URL)
			err := client.SetupK8sDiscovery(context.Background(), tt.clusterName)

			assertConnectRequest(t, cr, "/discovery.v1.DiscoveryService/SetupK8sDiscovery")
			assert.Contains(t, cr.Body, tt.clusterName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_RunK8sDiscovery(t *testing.T) { //nolint:dupl // Intentionally similar to GetAppInstrumentation — distinct endpoints.
	tests := []struct {
		name        string
		clusterName string
		respStatus  int
		respBody    string
		wantErr     bool
		wantNSCount int
	}{
		{
			name:        "returns discovered namespaces",
			clusterName: "prod-1",
			respStatus:  http.StatusOK,
			respBody:    `{"namespaces":[{"name":"default","apps":[{"name":"web","type":"deployment"}]}]}`,
			wantNSCount: 1,
		},
		{
			name:        "empty discovery result",
			clusterName: "empty",
			respStatus:  http.StatusOK,
			respBody:    `{}`,
			wantNSCount: 0,
		},
		{
			name:        "HTTP error returns error",
			clusterName: "bad",
			respStatus:  http.StatusInternalServerError,
			respBody:    `{"error":"server error"}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cr := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(srv.URL)
			resp, err := client.RunK8sDiscovery(context.Background(), tt.clusterName)

			assertConnectRequest(t, cr, "/discovery.v1.DiscoveryService/RunK8sDiscovery")
			assert.Contains(t, cr.Body, tt.clusterName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, resp.Namespaces, tt.wantNSCount)
		})
	}
}

func TestClient_RunK8sMonitoring(t *testing.T) {
	tests := []struct {
		name           string
		respStatus     int
		respBody       string
		wantErr        bool
		wantCluster    string
		wantClusterLen int
	}{
		{
			name:           "returns cluster states",
			respStatus:     http.StatusOK,
			respBody:       `{"clusters":[{"name":"prod-1","state":"active"}]}`,
			wantCluster:    "prod-1",
			wantClusterLen: 1,
		},
		{
			name:       "empty response",
			respStatus: http.StatusOK,
			respBody:   `{}`,
		},
		{
			name:       "HTTP error returns error",
			respStatus: http.StatusInternalServerError,
			respBody:   `{"error":"server error"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cr := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(srv.URL)
			resp, err := client.RunK8sMonitoring(context.Background())

			assertConnectRequest(t, cr, "/discovery.v1.DiscoveryService/RunK8sMonitoring")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantClusterLen > 0 {
				require.Len(t, resp.Clusters, tt.wantClusterLen)
				assert.Equal(t, tt.wantCluster, resp.Clusters[0].Name)
			}
		})
	}
}

func TestClient_AllEndpoints_RequestBodyContainsClusterName(t *testing.T) {
	// Extra check: each request body must include clusterName where applicable.
	tests := []struct {
		name           string
		invoke         func(client *instrumentation.Client, captured *capturedRequest, srv *httptest.Server) error
		wantPathSuffix string
	}{
		{
			name: "GetAppInstrumentation sends clusterName",
			invoke: func(client *instrumentation.Client, _ *capturedRequest, _ *httptest.Server) error {
				_, err := client.GetAppInstrumentation(context.Background(), "my-cluster")
				return err
			},
			wantPathSuffix: "/instrumentation.v1.InstrumentationService/GetAppInstrumentation",
		},
		{
			name: "SetK8SInstrumentation sends clusterName",
			invoke: func(client *instrumentation.Client, _ *capturedRequest, _ *httptest.Server) error {
				return client.SetK8SInstrumentation(context.Background(), "my-cluster", instrumentation.K8sSpec{})
			},
			wantPathSuffix: "/instrumentation.v1.InstrumentationService/SetK8SInstrumentation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				capturedBody = string(b)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			client := newTestClient(srv.URL)
			err := tt.invoke(client, nil, srv)
			require.NoError(t, err)

			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(capturedBody), &body))
			assert.Contains(t, body, "clusterName", "request body must contain clusterName field")
		})
	}
}
