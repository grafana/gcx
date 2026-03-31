package instrumentation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/setup/instrumentation"
	"github.com/grafana/gcx/internal/fleet"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// applyTestServer tracks which instrumentation endpoints are called.
type applyTestServer struct {
	getAppResp   string
	getAppStatus int
	setAppCalled atomic.Bool
	setK8sCalled atomic.Bool
	setAppStatus int
	setK8sStatus int
}

func (s *applyTestServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	if s.getAppStatus == 0 {
		s.getAppStatus = http.StatusOK
	}
	if s.setAppStatus == 0 {
		s.setAppStatus = http.StatusOK
	}
	if s.setK8sStatus == 0 {
		s.setK8sStatus = http.StatusOK
	}
	if s.getAppResp == "" {
		s.getAppResp = `{}`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/instrumentation.v1.InstrumentationService/GetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.getAppStatus)
		_, _ = w.Write([]byte(s.getAppResp))
	})
	mux.HandleFunc("/instrumentation.v1.InstrumentationService/SetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		s.setAppCalled.Store(true)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.setAppStatus)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/instrumentation.v1.InstrumentationService/SetK8SInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		s.setK8sCalled.Store(true)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.setK8sStatus)
		_, _ = w.Write([]byte(`{}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func makeApplyClient(serverURL string) *instrum.Client {
	f := fleet.NewClient(serverURL, "inst-id", "api-token", true, nil)
	return instrum.NewClient(f)
}

func writeApplyManifest(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

const (
	manifestAppOnly = `apiVersion: setup.grafana.app/v1alpha1
kind: InstrumentationConfig
metadata:
  name: prod-1
spec:
  app:
    namespaces:
      - name: frontend
        selection: included
        tracing: true
`
	manifestK8sOnly = `apiVersion: setup.grafana.app/v1alpha1
kind: InstrumentationConfig
metadata:
  name: prod-1
spec:
  k8s:
    costmetrics: true
    clusterevents: true
`
	manifestBoth = `apiVersion: setup.grafana.app/v1alpha1
kind: InstrumentationConfig
metadata:
  name: prod-1
spec:
  app:
    namespaces:
      - name: frontend
        selection: included
        tracing: true
  k8s:
    costmetrics: true
`
)

func TestRunApply_AppOnly(t *testing.T) {
	ts := &applyTestServer{}
	srv := ts.start(t)

	err := instrumentation.RunApply(context.Background(), &instrumentation.ApplyOpts{File: writeApplyManifest(t, manifestAppOnly)}, makeApplyClient(srv.URL), instrum.BackendURLs{}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.True(t, ts.setAppCalled.Load(), "SetAppInstrumentation must be called")
	assert.False(t, ts.setK8sCalled.Load(), "SetK8SInstrumentation must NOT be called when spec.k8s is absent")
}

func TestRunApply_K8sOnly(t *testing.T) {
	ts := &applyTestServer{}
	srv := ts.start(t)

	err := instrumentation.RunApply(context.Background(), &instrumentation.ApplyOpts{File: writeApplyManifest(t, manifestK8sOnly)}, makeApplyClient(srv.URL), instrum.BackendURLs{}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.False(t, ts.setAppCalled.Load(), "SetAppInstrumentation must NOT be called when spec.app is absent")
	assert.True(t, ts.setK8sCalled.Load(), "SetK8SInstrumentation must be called")
}

func TestRunApply_BothSections(t *testing.T) {
	ts := &applyTestServer{}
	srv := ts.start(t)

	err := instrumentation.RunApply(context.Background(), &instrumentation.ApplyOpts{File: writeApplyManifest(t, manifestBoth)}, makeApplyClient(srv.URL), instrum.BackendURLs{}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.True(t, ts.setAppCalled.Load(), "SetAppInstrumentation must be called")
	assert.True(t, ts.setK8sCalled.Load(), "SetK8SInstrumentation must be called")
}

func TestRunApply_OptimisticLockFailure(t *testing.T) {
	remoteNamespaces, err := json.Marshal(map[string]any{
		"cluster": map[string]any{
			"name": "cluster-name",
			"namespaces": []map[string]any{
				{"name": "monitoring", "selection": "included"},
			},
		},
	})
	require.NoError(t, err)

	ts := &applyTestServer{getAppResp: string(remoteNamespaces)}
	srv := ts.start(t)

	// Local manifest only has "frontend"; remote has "monitoring" — optimistic lock must fail.
	err = instrumentation.RunApply(context.Background(), &instrumentation.ApplyOpts{File: writeApplyManifest(t, manifestAppOnly)}, makeApplyClient(srv.URL), instrum.BackendURLs{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setup/instrumentation:")
	assert.Contains(t, err.Error(), `"monitoring"`)
	assert.Contains(t, err.Error(), "not in local manifest")
	assert.False(t, ts.setAppCalled.Load(), "SetAppInstrumentation must NOT be called on optimistic lock failure")
}

func TestRunApply_LocalSupersetOfRemote(t *testing.T) {
	// Remote has "frontend"; local adds "backend" — superset should succeed.
	remoteNamespaces, err := json.Marshal(map[string]any{
		"cluster": map[string]any{
			"name": "cluster-name",
			"namespaces": []map[string]any{
				{"name": "frontend", "selection": "included", "tracing": true},
			},
		},
	})
	require.NoError(t, err)

	ts := &applyTestServer{getAppResp: string(remoteNamespaces)}
	srv := ts.start(t)

	const superset = `apiVersion: setup.grafana.app/v1alpha1
kind: InstrumentationConfig
metadata:
  name: prod-1
spec:
  app:
    namespaces:
      - name: frontend
        selection: included
        tracing: true
      - name: backend
        selection: included
`
	err = instrumentation.RunApply(context.Background(), &instrumentation.ApplyOpts{File: writeApplyManifest(t, superset)}, makeApplyClient(srv.URL), instrum.BackendURLs{}, &bytes.Buffer{})
	require.NoError(t, err)
	assert.True(t, ts.setAppCalled.Load(), "SetAppInstrumentation must be called when local is superset of remote")
}

func TestRunApply_DryRun(t *testing.T) {
	ts := &applyTestServer{}
	srv := ts.start(t)

	var out bytes.Buffer
	err := instrumentation.RunApply(context.Background(), &instrumentation.ApplyOpts{File: writeApplyManifest(t, manifestBoth), DryRun: true}, makeApplyClient(srv.URL), instrum.BackendURLs{}, &out)
	require.NoError(t, err)
	assert.False(t, ts.setAppCalled.Load(), "SetAppInstrumentation must NOT be called during dry-run")
	assert.False(t, ts.setK8sCalled.Load(), "SetK8SInstrumentation must NOT be called during dry-run")
	assert.Contains(t, out.String(), "dry-run")
}

func TestRunApply_InvalidAPIVersion(t *testing.T) {
	const bad = `apiVersion: wrong/v1
kind: InstrumentationConfig
metadata:
  name: prod-1
`
	err := instrumentation.RunApply(context.Background(), &instrumentation.ApplyOpts{File: writeApplyManifest(t, bad)}, nil, instrum.BackendURLs{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setup/instrumentation:")
	assert.Contains(t, err.Error(), "apiVersion")
}

func TestRunApply_MissingMetadataName(t *testing.T) {
	const noName = `apiVersion: setup.grafana.app/v1alpha1
kind: InstrumentationConfig
metadata:
  name: ""
`
	err := instrumentation.RunApply(context.Background(), &instrumentation.ApplyOpts{File: writeApplyManifest(t, noName)}, nil, instrum.BackendURLs{}, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setup/instrumentation:")
	assert.Contains(t, err.Error(), "metadata.name")
}

func TestApplyOpts_Validate_MissingFile(t *testing.T) {
	opts := &instrumentation.ApplyOpts{}
	err := opts.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setup/instrumentation:")
	assert.Contains(t, err.Error(), "-f/--filename is required")
}

// TestRunApply_ManifestHasNoStackSpecificValues verifies that InstrumentationConfig
// manifests do not contain datasource URLs, instance IDs, or API tokens (NC-003).
func TestRunApply_ManifestHasNoStackSpecificValues(t *testing.T) {
	path := writeApplyManifest(t, manifestAppOnly)
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	content := string(data)
	assert.NotContains(t, content, "datasourceURL")
	assert.NotContains(t, content, "instanceID")
	assert.NotContains(t, content, "apiToken")
	assert.NotContains(t, content, "stackID")

	// Additionally verify the typed struct fields match what we expect.
	var config instrum.InstrumentationConfig
	require.NoError(t, yaml.Unmarshal(data, &config))
	assert.Equal(t, "prod-1", config.Metadata.Name)
	assert.NotNil(t, config.Spec.App)
	assert.Nil(t, config.Spec.K8s)
}
