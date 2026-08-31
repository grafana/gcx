package fleet_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

const (
	proxyPrefix   = "/api/plugin-proxy/grafana-collector-app/fleet-management-api"
	instancesPath = "/api/plugin-proxy/grafana-collector-app/grafanacom-api/instances/"
)

// fakeLoader returns a stack REST config pointing at a test server.
type fakeLoader struct {
	host      string
	namespace string
	err       error
}

func (f fakeLoader) LoadGrafanaConfig(_ context.Context) (config.NamespacedRESTConfig, error) {
	if f.err != nil {
		return config.NamespacedRESTConfig{}, f.err
	}
	return config.NamespacedRESTConfig{
		Config:    rest.Config{Host: f.host},
		Namespace: f.namespace,
	}, nil
}

const stackInfoJSON = `{
	"id": 1631916,
	"slug": "wbkprez",
	"orgSlug": "wardbekker",
	"regionSlug": "prod-eu-west-2",
	"hmInstancePromId": 3188056,
	"hmInstancePromUrl": "https://prometheus-prod-65-prod-eu-west-2.grafana.net",
	"hmInstancePromClusterId": 417,
	"hlInstanceId": 1589703,
	"hlInstanceUrl": "https://logs-prod-012.grafana.net",
	"agentManagementInstanceId": 1631916,
	"agentManagementInstanceUrl": "https://fleet-management-prod-011.grafana.net"
}`

func TestLoadClient(t *testing.T) {
	var stackInfoCalls atomic.Int64
	var rpcPath atomic.Value

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == instancesPath {
			stackInfoCalls.Add(1)
		} else {
			rpcPath.Store(r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client, namespace, err := fleet.LoadClient(context.Background(), fakeLoader{host: server.URL, namespace: "stack-1"})
	require.NoError(t, err)
	assert.Equal(t, "stack-1", namespace)

	// LoadClient must not fetch the stack record. Only LoadClientWithStack does.
	assert.Equal(t, int64(0), stackInfoCalls.Load())

	resp, err := client.DoRequest(context.Background(), "/pipeline.v1.PipelineService/ListPipelines", nil)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, proxyPrefix+"/pipeline.v1.PipelineService/ListPipelines", rpcPath.Load())
}

func TestLoadClient_LoaderError(t *testing.T) {
	_, _, err := fleet.LoadClient(context.Background(), fakeLoader{err: assert.AnError})
	require.ErrorIs(t, err, assert.AnError)
}

func TestLoadClientWithStack(t *testing.T) {
	var stackInfoCalls atomic.Int64

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != instancesPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		stackInfoCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(stackInfoJSON))
	}))
	defer server.Close()

	loader := fakeLoader{host: server.URL, namespace: "stack-1"}

	result, err := fleet.LoadClientWithStack(context.Background(), loader)
	require.NoError(t, err)

	assert.Equal(t, "stack-1", result.Namespace)
	assert.Equal(t, "wbkprez", result.Stack.Slug)
	assert.Equal(t, "wardbekker", result.Stack.OrgSlug)
	assert.Equal(t, 3188056, result.Stack.HMInstancePromID)
	assert.Equal(t, 417, result.Stack.HMInstancePromClusterID)
	assert.Equal(t, 1589703, result.Stack.HLInstanceID)
	assert.Equal(t, "https://fleet-management-prod-011.grafana.net", result.Stack.AgentManagementInstanceURL)
	assert.Equal(t, 1631916, result.Stack.AgentManagementInstanceID)

	// A second load reuses the memoized record for the same host.
	_, err = fleet.LoadClientWithStack(context.Background(), loader)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stackInfoCalls.Load())
}

func TestLoadClientWithStack_PluginMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"plugin route match not found"}`))
	}))
	defer server.Close()

	_, err := fleet.LoadClientWithStack(context.Background(), fakeLoader{host: server.URL})
	require.Error(t, err)

	var httpErr *fleet.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusNotFound, httpErr.Status)
	assert.Contains(t, httpErr.Body, fleet.PluginRouteMissingMarker)
}
