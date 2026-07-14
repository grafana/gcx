package instrumentation_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

// proxyFleetBase is the collector-app fleet-management-api proxy route prefix
// prepended to every Connect service/method path by the base client.
const proxyFleetBase = "/api/plugin-proxy/grafana-collector-app/fleet-management-api"

// newTestClient creates an instrumentation Client pointed at the given test
// server URL. The Grafana bearer credential ("test-bearer") is injected by the
// k8s round-tripper — the client sets no Basic auth of its own.
func newTestClient(t *testing.T, serverURL string) *instrumentation.Client {
	t.Helper()
	f, err := fleet.NewClient(config.NamespacedRESTConfig{
		Config: rest.Config{Host: serverURL, BearerToken: "test-bearer"},
	})
	require.NoError(t, err)
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
		cr.Authorization = r.Header.Get("Authorization")
		_, _, cr.HasBasicAuth = r.BasicAuth()
		cr.PromClusterID = r.Header.Get("X-Prom-Cluster-Id")
		cr.PromInstanceID = r.Header.Get("X-Prom-Instance-Id")
		cr.ScopeOrgID = r.Header.Get("X-Scope-Orgid")
		b, _ := io.ReadAll(r.Body)
		cr.Body = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(respBody))
	}, cr
}

type capturedRequest struct {
	Method         string
	Path           string
	ContentType    string
	Accept         string
	Authorization  string
	HasBasicAuth   bool
	PromClusterID  string
	PromInstanceID string
	ScopeOrgID     string
	Body           string
}

// assertConnectRequest verifies that a captured request conforms to the Connect
// RPC over HTTP POST contract (NC-006) AND the collector-app plugin-proxy
// transport contract: POST, JSON content/accept, the full proxy-prefixed URL,
// the round-tripper-injected Grafana bearer, and no client-set Basic auth or
// X-Prom-*/X-Scope-OrgID headers (FR-001, FR-003, FR-007, FR-017).
func assertConnectRequest(t *testing.T, cr *capturedRequest, wantPath string) {
	t.Helper()
	assert.Equal(t, http.MethodPost, cr.Method, "must use POST")
	assert.Equal(t, "application/json", cr.ContentType, "must set Content-Type: application/json")
	assert.Equal(t, "application/json", cr.Accept, "must set Accept: application/json")
	assert.Equal(t, proxyFleetBase+wantPath, cr.Path, "must target the collector-app fleet-management-api proxy route with the unchanged Connect path suffix")
	assert.Equal(t, "Bearer test-bearer", cr.Authorization, "bearer must be injected by the round-tripper")
	assert.False(t, cr.HasBasicAuth, "client must not set Basic auth")
	assert.Empty(t, cr.PromClusterID, "client must not set X-Prom-Cluster-ID")
	assert.Empty(t, cr.PromInstanceID, "client must not set X-Prom-Instance-ID")
	assert.Empty(t, cr.ScopeOrgID, "client must not set X-Scope-OrgID")
}

func TestClient_GetAppInstrumentation(t *testing.T) {
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
			respBody:    `{"cluster":{"name":"prod-1","namespaces":[{"name":"default","autoinstrument":true,"tracing":true}]}}`,
			wantNSCount: 1,
		},
		{
			name:        "empty namespaces on 200",
			clusterName: "empty-cluster",
			respStatus:  http.StatusOK,
			respBody:    `{"cluster":{"name":"empty-cluster"}}`,
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

			client := newTestClient(t, srv.URL)
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

// TestClient_GetAppInstrumentation_Converts verifies that wire bool fields are
// converted to *bool in the domain type (D-06).
func TestClient_GetAppInstrumentation_Converts(t *testing.T) {
	respBody := `{"cluster":{"name":"c1","namespaces":[{"name":"default","autoinstrument":true,"tracing":true,"logging":false,"processmetrics":false,"extendedmetrics":false,"profiling":false}]}}`

	handler, _ := captureHandler(t, http.StatusOK, respBody)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := newTestClient(t, srv.URL).GetAppInstrumentation(context.Background(), "c1")
	require.NoError(t, err)
	require.Len(t, resp.Namespaces, 1)

	ns := resp.Namespaces[0]
	assert.Equal(t, "default", ns.Name)
	// Wire bool true → *bool true
	require.NotNil(t, ns.Autoinstrument)
	assert.True(t, *ns.Autoinstrument)
	// Wire bool false → *bool false (not nil)
	require.NotNil(t, ns.Logging)
	assert.False(t, *ns.Logging)
}

func TestClient_SetAppInstrumentation(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		namespaces  []instrumentation.App
		respStatus  int
		respBody    string
		wantErr     bool
	}{
		{
			name:        "successful set",
			clusterName: "prod-1",
			namespaces: []instrumentation.App{
				{Name: "default", Autoinstrument: boolPtr(true), Tracing: boolPtr(true)}, //nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
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

			client := newTestClient(t, srv.URL)
			err := client.SetAppInstrumentation(context.Background(), tt.clusterName, tt.namespaces, instrumentation.BackendURLs{})

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
			respBody:    `{"cluster":{"name":"prod-1","selection":"SELECTION_INCLUDED","costmetrics":true,"clusterevents":false}}`,
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

			client := newTestClient(t, srv.URL)
			resp, err := client.GetK8SInstrumentation(context.Background(), tt.clusterName)

			assertConnectRequest(t, cr, "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation")
			assert.Contains(t, cr.Body, tt.clusterName)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, resp.Cluster.CostMetrics)
			assert.Equal(t, tt.wantCost, *resp.Cluster.CostMetrics)
			// Selection must always be populated from the wire response (FR-013)
			assert.Equal(t, "SELECTION_INCLUDED", resp.Cluster.Selection)
		})
	}
}

func TestClient_SetK8SInstrumentation(t *testing.T) {
	tests := []struct {
		name        string
		clusterName string
		k8s         instrumentation.Cluster
		respStatus  int
		respBody    string
		wantErr     bool
	}{
		{
			name:        "successful set",
			clusterName: "prod-1",
			k8s:         instrumentation.Cluster{CostMetrics: boolPtr(true), NodeLogs: boolPtr(true)}, //nolint:modernize // ptr() creates pointer to value, not pointer to type like new()
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

			client := newTestClient(t, srv.URL)
			err := client.SetK8SInstrumentation(context.Background(), tt.clusterName, tt.k8s, instrumentation.BackendURLs{})

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

// TestGetK8SInstrumentation_SelectionRoundTrip verifies that the response mapper
// correctly preserves SELECTION_EXCLUDED when the server returns it.
// This is the symmetric case for SELECTION_INCLUDED tested in TestClient_GetK8SInstrumentation.
// Theme E (2026-04-30): if the live stack returns "" after clusters remove, the bug
// is server-side — not in this mapper. See docs/adrs/instrumentation/002-cli-redesign.md § "E — Selection enum hygiene".
func TestGetK8SInstrumentation_SelectionRoundTrip(t *testing.T) {
	wireResp := `{"cluster":{"name":"test-cluster","selection":"SELECTION_EXCLUDED","costmetrics":false,"energymetrics":false,"clusterevents":false,"nodelogs":false}}`
	handler, _ := captureHandler(t, http.StatusOK, wireResp)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := newTestClient(t, srv.URL).GetK8SInstrumentation(context.Background(), "test-cluster")
	require.NoError(t, err)
	assert.Equal(t, "SELECTION_EXCLUDED", resp.Cluster.Selection,
		"Selection must survive wire→domain mapping")
}

// TestSetK8SInstrumentation_SendsSelectionExcluded verifies that SetK8SInstrumentation
// sends "SELECTION_EXCLUDED" verbatim in the request body when that value is set.
func TestSetK8SInstrumentation_SendsSelectionExcluded(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	excluded := instrumentation.Cluster{
		Name:      "test-cluster",
		Selection: "SELECTION_EXCLUDED",
	}
	err := newTestClient(t, srv.URL).SetK8SInstrumentation(context.Background(), "test-cluster", excluded, instrumentation.BackendURLs{})
	require.NoError(t, err)
	assert.Contains(t, capturedBody, `"SELECTION_EXCLUDED"`,
		"SetK8SInstrumentation must send SELECTION_EXCLUDED in request body")
}

// TestClient_SetK8SInstrumentation_DefaultsSelection verifies that when Cluster.Selection
// is empty, the wire request uses SELECTION_INCLUDED.
func TestClient_SetK8SInstrumentation_DefaultsSelection(t *testing.T) {
	var capturedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		capturedBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	err := newTestClient(t, srv.URL).SetK8SInstrumentation(context.Background(), "c1",
		instrumentation.Cluster{Name: "c1"}, instrumentation.BackendURLs{})
	require.NoError(t, err)
	assert.Contains(t, capturedBody, "SELECTION_INCLUDED", "empty Selection must default to SELECTION_INCLUDED")
}

func TestClient_SetupK8sDiscovery(t *testing.T) {
	tests := []struct {
		name       string
		respStatus int
		respBody   string
		wantErr    bool
	}{
		{
			name:       "successful setup",
			respStatus: http.StatusOK,
			respBody:   `{}`,
		},
		{
			name:       "HTTP error returns error",
			respStatus: http.StatusForbidden,
			respBody:   `{"error":"forbidden"}`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, cr := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			err := client.SetupK8sDiscovery(context.Background(),
				instrumentation.BackendURLs{})

			assertConnectRequest(t, cr, "/discovery.v1.DiscoveryService/SetupK8sDiscovery")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestClient_RunK8sDiscovery(t *testing.T) {
	tests := []struct {
		name          string
		respStatus    int
		respBody      string
		wantErr       bool
		wantItemCount int
	}{
		{
			name:          "returns discovered items",
			respStatus:    http.StatusOK,
			respBody:      `{"items":[{"clusterName":"cluster-1","namespace":"default","name":"web","workloadType":"deployment","instrumentationStatus":"INSTRUMENTED"}]}`,
			wantItemCount: 1,
		},
		{
			name:          "empty discovery result",
			respStatus:    http.StatusOK,
			respBody:      `{}`,
			wantItemCount: 0,
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

			client := newTestClient(t, srv.URL)
			resp, err := client.RunK8sDiscovery(context.Background())

			assertConnectRequest(t, cr, "/discovery.v1.DiscoveryService/RunK8sDiscovery")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, resp.Items, tt.wantItemCount)
		})
	}
}

// TestClient_IsNamespaceDiscovered verifies the IsNamespaceDiscovered helper
// that cross-references RunK8sDiscovery items for the (cluster, namespace) pair.
func TestClient_IsNamespaceDiscovered(t *testing.T) {
	// Discovery response with one item for cluster "c1" / namespace "ns1".
	discoveryBody := `{"items":[
		{"clusterName":"c1","namespace":"ns1","name":"web"},
		{"clusterName":"c1","namespace":"ns2","name":"api"},
		{"clusterName":"c2","namespace":"ns1","name":"svc"}
	]}`

	tests := []struct {
		name       string
		respStatus int
		respBody   string
		cluster    string
		namespace  string
		wantResult bool
		wantErr    bool
	}{
		{
			name:       "cluster present, namespace present — true",
			respStatus: http.StatusOK,
			respBody:   discoveryBody,
			cluster:    "c1",
			namespace:  "ns1",
			wantResult: true,
		},
		{
			name:       "cluster present, namespace absent — false",
			respStatus: http.StatusOK,
			respBody:   discoveryBody,
			cluster:    "c1",
			namespace:  "missing",
			wantResult: false,
		},
		{
			name:       "cluster absent — false",
			respStatus: http.StatusOK,
			respBody:   discoveryBody,
			cluster:    "unknown-cluster",
			namespace:  "ns1",
			wantResult: false,
		},
		{
			name:       "same namespace name under different cluster — false",
			respStatus: http.StatusOK,
			respBody:   discoveryBody,
			cluster:    "c2",
			namespace:  "ns2",
			wantResult: false,
		},
		{
			name:       "RPC error propagates",
			respStatus: http.StatusInternalServerError,
			respBody:   `{"error":"server error"}`,
			cluster:    "c1",
			namespace:  "ns1",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := captureHandler(t, tt.respStatus, tt.respBody)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			client := newTestClient(t, srv.URL)
			got, err := client.IsNamespaceDiscovered(context.Background(),
				tt.cluster, tt.namespace)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantResult, got)
		})
	}
}

// TestClient_RunK8sDiscovery_StatusConversion verifies that InstrumentationStatus
// is properly converted from wire string to typed constant.
func TestClient_RunK8sDiscovery_StatusConversion(t *testing.T) {
	respBody := `{"items":[{"clusterName":"c1","name":"web","instrumentationStatus":"PENDING_INSTRUMENTATION"}]}`
	handler, _ := captureHandler(t, http.StatusOK, respBody)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := newTestClient(t, srv.URL).RunK8sDiscovery(context.Background())
	require.NoError(t, err)
	require.Len(t, resp.Items, 1)
	assert.Equal(t, instrumentation.StatusPendingInstrumentation, resp.Items[0].InstrumentationStatus)
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
			respBody:       `{"clusters":[{"name":"prod-1","instrumentationStatus":"INSTRUMENTED"}]}`,
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

			client := newTestClient(t, srv.URL)
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

func TestClient_ListPipelines(t *testing.T) {
	tests := []struct {
		name       string
		respStatus int
		respBody   string
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "returns pipelines",
			respStatus: http.StatusOK,
			respBody:   `{"pipelines":[{"id":"abc","name":"k8s-monitoring","metadata":{"type":"k8s_monitoring","cluster":"prod-1"}}]}`,
			wantCount:  1,
		},
		{
			name:       "empty pipeline list",
			respStatus: http.StatusOK,
			respBody:   `{}`,
			wantCount:  0,
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

			client := newTestClient(t, srv.URL)
			pipelines, err := client.ListPipelines(context.Background())

			assertConnectRequest(t, cr, "/pipeline.v1.PipelineService/ListPipelines")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, pipelines, tt.wantCount)
		})
	}
}

// TestClient_ListPipelines_MetadataPreserved verifies that pipeline metadata
// is preserved so the enumerate helper can filter by it (D-14).
func TestClient_ListPipelines_MetadataPreserved(t *testing.T) {
	respBody := `{"pipelines":[{"id":"abc","name":"k8s-monitoring","metadata":{"type":"k8s_monitoring","cluster":"prod-1"}}]}`
	handler, _ := captureHandler(t, http.StatusOK, respBody)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	pipelines, err := newTestClient(t, srv.URL).ListPipelines(context.Background())
	require.NoError(t, err)
	require.Len(t, pipelines, 1)
	assert.Equal(t, "abc", pipelines[0].ID)
	assert.Equal(t, "k8s_monitoring", pipelines[0].Metadata["type"])
	assert.Equal(t, "prod-1", pipelines[0].Metadata["cluster"])
}

// TestClient_AllEndpoints_RequestBodyContainsClusterName verifies that each
// relevant endpoint sends the cluster name in the request body.
func TestClient_AllEndpoints_RequestBodyContainsClusterName(t *testing.T) {
	tests := []struct {
		name           string
		invoke         func(client *instrumentation.Client) error
		wantPathSuffix string
		checkBody      func(t *testing.T, body map[string]json.RawMessage)
	}{
		{
			name: "GetAppInstrumentation sends cluster_name",
			invoke: func(client *instrumentation.Client) error {
				_, err := client.GetAppInstrumentation(context.Background(), "my-cluster")
				return err
			},
			wantPathSuffix: "/instrumentation.v1.InstrumentationService/GetAppInstrumentation",
			checkBody: func(t *testing.T, body map[string]json.RawMessage) {
				t.Helper()
				assert.Contains(t, body, "cluster_name", "request body must contain cluster_name field (snake_case)")
			},
		},
		{
			name: "SetK8SInstrumentation sends cluster envelope",
			invoke: func(client *instrumentation.Client) error {
				return client.SetK8SInstrumentation(context.Background(), "my-cluster",
					instrumentation.Cluster{}, instrumentation.BackendURLs{})
			},
			wantPathSuffix: "/instrumentation.v1.InstrumentationService/SetK8SInstrumentation",
			checkBody: func(t *testing.T, body map[string]json.RawMessage) {
				t.Helper()
				assert.Contains(t, body, "cluster", "request body must contain cluster envelope")
			},
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

			err := tt.invoke(newTestClient(t, srv.URL))
			require.NoError(t, err)

			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(capturedBody), &body))
			tt.checkBody(t, body)
		})
	}
}
