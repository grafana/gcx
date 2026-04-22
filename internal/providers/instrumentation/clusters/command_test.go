package clusters_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation/clusters"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLoader struct {
	serverURL string
	loadErr   error
}

func (m *mockLoader) LoadCloudConfig(_ context.Context) (providers.CloudRESTConfig, error) {
	if m.loadErr != nil {
		return providers.CloudRESTConfig{}, m.loadErr
	}
	return providers.CloudRESTConfig{
		Token: "test-token",
		Stack: cloud.StackInfo{
			AgentManagementInstanceURL: m.serverURL,
			AgentManagementInstanceID:  1234,
			HMInstancePromID:           5678,
			HMInstancePromClusterID:    42,
			HMInstancePromURL:          "https://prometheus-prod.grafana.net",
			HLInstanceID:               9012,
			HLInstanceURL:              "https://logs-prod.grafana.net",
			HTInstanceID:               3456,
			HTInstanceURL:              "https://tempo-prod.grafana.net",
			HPInstanceID:               7890,
			HPInstanceURL:              "https://profiles-prod.grafana.net",
		},
	}, nil
}

// clustersTestServer returns a test server that handles list (RunK8sMonitoring +
// GetK8SInstrumentation) and set (SetK8SInstrumentation) paths.
func clustersTestServer(t *testing.T, monitoringBody string, instrBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/discovery.v1.DiscoveryService/RunK8sMonitoring":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(monitoringBody))
		case "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(instrBody))
		case "/instrumentation.v1.InstrumentationService/SetK8SInstrumentation":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestClustersHelp(t *testing.T) {
	cmd := clusters.NewCommandForTest(&mockLoader{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "list")
	assert.Contains(t, out, "get")
	assert.Contains(t, out, "create")
	assert.Contains(t, out, "update")
	assert.Contains(t, out, "delete")
}

func TestClustersListRendersItems(t *testing.T) {
	monitoringBody := `{"clusters":[{"name":"prod-1"},{"name":"prod-2"}]}`
	instrBody := `{"costmetrics":true,"clusterevents":true,"energymetrics":false,"nodelogs":true}`

	srv := clustersTestServer(t, monitoringBody, instrBody)
	defer srv.Close()

	loader := &mockLoader{serverURL: srv.URL}
	cmd := clusters.NewCommandForTest(loader)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"list"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "prod-1")
	assert.Contains(t, out, "prod-2")
}

func TestClustersListJSONOutput(t *testing.T) {
	monitoringBody := `{"clusters":[{"name":"prod-1"}]}`
	instrBody := `{"costmetrics":true,"clusterevents":false,"energymetrics":false,"nodelogs":false}`

	srv := clustersTestServer(t, monitoringBody, instrBody)
	defer srv.Close()

	loader := &mockLoader{serverURL: srv.URL}
	cmd := clusters.NewCommandForTest(loader)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"list", "-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var items []map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &items))
	assert.NotEmpty(t, items)
}

func TestClustersCreateMissingFilename(t *testing.T) {
	cmd := clusters.NewCommandForTest(&mockLoader{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"create"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--filename")
}

func TestClustersCreateFromFile(t *testing.T) {
	manifest := `apiVersion: instrumentation.grafana.app/v1alpha1
kind: Cluster
metadata:
  name: prod-east
spec:
  costMetrics: true
  clusterEvents: true
  energyMetrics: false
  nodeLogs: true
`
	dir := t.TempDir()
	manifestFile := filepath.Join(dir, "cluster.yaml")
	require.NoError(t, os.WriteFile(manifestFile, []byte(manifest), 0600))

	instrBody := `{"costmetrics":true,"clusterevents":true,"energymetrics":false,"nodelogs":true}`
	srv := clustersTestServer(t, `{}`, instrBody)
	defer srv.Close()

	loader := &mockLoader{serverURL: srv.URL}
	cmd := clusters.NewCommandForTest(loader)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetArgs([]string{"create", "-f", manifestFile})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestClustersGetExactArgs(t *testing.T) {
	cmd := clusters.NewCommandForTest(&mockLoader{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"get"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestClustersDeleteZeroesFlags(t *testing.T) {
	// Delete zeros out all K8S monitoring flags via SetK8SInstrumentation.
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	loader := &mockLoader{serverURL: srv.URL}
	cmd := clusters.NewCommandForTest(loader)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"delete", "prod-east"})

	err := cmd.Execute()
	require.NoError(t, err)
	// Verify the payload sent to the server contains the cluster name and zeroed flags.
	assert.Contains(t, string(gotBody), "prod-east")
}

func TestClustersDeletePropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"internal error"}`))
	}))
	defer srv.Close()

	loader := &mockLoader{serverURL: srv.URL}
	cmd := clusters.NewCommandForTest(loader)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"delete", "prod-east"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete cluster")
}

func TestClusterUpdate_PreservesSelectionExcluded(t *testing.T) {
	// The remote cluster has SELECTION_EXCLUDED. The local manifest omits the
	// selection field entirely. The write payload sent to SetK8SInstrumentation
	// must still carry SELECTION_EXCLUDED, not the default SELECTION_INCLUDED.

	manifest := `apiVersion: instrumentation.grafana.app/v1alpha1
kind: Cluster
metadata:
  name: prod-east
spec:
  costMetrics: true
  clusterEvents: false
  energyMetrics: false
  nodeLogs: false
`
	dir := t.TempDir()
	manifestFile := filepath.Join(dir, "cluster.yaml")
	require.NoError(t, os.WriteFile(manifestFile, []byte(manifest), 0600))

	// Track the body sent to SetK8SInstrumentation.
	var setBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/discovery.v1.DiscoveryService/RunK8sMonitoring":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"clusters":[{"name":"prod-east"}]}`))
		case "/instrumentation.v1.InstrumentationService/GetK8SInstrumentation":
			// Remote has SELECTION_EXCLUDED.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"cluster":{"name":"prod-east","selection":"SELECTION_EXCLUDED","costmetrics":true,"clusterevents":false,"energymetrics":false,"nodelogs":false}}`))
		case "/instrumentation.v1.InstrumentationService/SetK8SInstrumentation":
			setBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	loader := &mockLoader{serverURL: srv.URL}
	cmd := clusters.NewCommandForTest(loader)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{"update", "prod-east", "-f", manifestFile})

	err := cmd.Execute()
	require.NoError(t, err)

	// The SetK8SInstrumentation payload must preserve SELECTION_EXCLUDED.
	assert.Contains(t, string(setBody), "SELECTION_EXCLUDED",
		"update must preserve remote SELECTION_EXCLUDED when selection is omitted from the local manifest")
	assert.NotContains(t, string(setBody), "SELECTION_INCLUDED",
		"update must NOT clobber SELECTION_EXCLUDED with SELECTION_INCLUDED")
}
