package instrumentation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cmdinstr "github.com/grafana/gcx/cmd/gcx/setup/instrumentation"
	"github.com/grafana/gcx/internal/fleet"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// showTestServer is an httptest.Server that returns configurable responses
// for GetAppInstrumentation and GetK8SInstrumentation endpoints.
type showTestServer struct {
	getAppResp   string
	getAppStatus int
	getK8sResp   string
	getK8sStatus int
}

func (s *showTestServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	if s.getAppStatus == 0 {
		s.getAppStatus = http.StatusOK
	}
	if s.getK8sStatus == 0 {
		s.getK8sStatus = http.StatusOK
	}
	if s.getAppResp == "" {
		s.getAppResp = `{}`
	}
	if s.getK8sResp == "" {
		s.getK8sResp = `{}`
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/instrumentation.v1.InstrumentationService/GetAppInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.getAppStatus)
		_, _ = w.Write([]byte(s.getAppResp))
	})
	mux.HandleFunc("/instrumentation.v1.InstrumentationService/GetK8SInstrumentation", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.getK8sStatus)
		_, _ = w.Write([]byte(s.getK8sResp))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func makeShowClient(serverURL string) *instrum.Client {
	f := fleet.NewClient(serverURL, "inst-id", "api-token", true, nil)
	return instrum.NewClient(f)
}

func unmarshalTestYAML(data []byte, v any) error {
	return yaml.Unmarshal(data, v)
}

// TestRunShow_AppAndK8sConfig verifies that show produces a valid YAML manifest
// when a cluster has both app and k8s instrumentation configured.
func TestRunShow_AppAndK8sConfig(t *testing.T) {
	ts := &showTestServer{
		getAppResp: `{"cluster":{"name":"prod-1","namespaces":[{"name":"frontend","selection":"included","tracing":true},{"name":"data","selection":"included"}]}}`,
		getK8sResp: `{"cluster":{"name":"prod-1","costmetrics":true,"clusterevents":true}}`,
	}
	srv := ts.start(t)

	opts := &cmdinstr.ShowOpts{}
	opts.IO.OutputFormat = "yaml"

	var out bytes.Buffer
	err := cmdinstr.RunShow(context.Background(), opts, makeShowClient(srv.URL), "prod-1", &out)
	require.NoError(t, err)

	output := out.String()
	assert.Contains(t, output, "apiVersion: setup.grafana.app/v1alpha1")
	assert.Contains(t, output, "kind: InstrumentationConfig")
	assert.Contains(t, output, "name: prod-1")
	assert.Contains(t, output, "frontend")
	assert.Contains(t, output, "data")
	assert.Contains(t, output, "costmetrics: true")

	// Verify it parses as valid YAML InstrumentationConfig.
	var cfg instrum.InstrumentationConfig
	require.NoError(t, unmarshalTestYAML(out.Bytes(), &cfg))
	assert.Equal(t, instrum.APIVersion, cfg.APIVersion)
	assert.Equal(t, instrum.Kind, cfg.Kind)
	assert.Equal(t, "prod-1", cfg.Metadata.Name)
	require.NotNil(t, cfg.Spec.App)
	assert.Len(t, cfg.Spec.App.Namespaces, 2)
	require.NotNil(t, cfg.Spec.K8s)
	assert.True(t, cfg.Spec.K8s.CostMetrics)
}

// TestRunShow_JSONOutput verifies that show produces valid JSON when output format is json.
func TestRunShow_JSONOutput(t *testing.T) {
	ts := &showTestServer{
		getAppResp: `{}`,
		getK8sResp: `{"cluster":{"name":"prod-1","costmetrics":true,"nodelogs":true}}`,
	}
	srv := ts.start(t)

	opts := &cmdinstr.ShowOpts{}
	opts.IO.OutputFormat = "json"

	var out bytes.Buffer
	err := cmdinstr.RunShow(context.Background(), opts, makeShowClient(srv.URL), "prod-1", &out)
	require.NoError(t, err)

	// Verify valid JSON.
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(out.Bytes(), &raw), "output must be valid JSON")

	// Parse as InstrumentationConfig.
	var cfg instrum.InstrumentationConfig
	require.NoError(t, json.Unmarshal(out.Bytes(), &cfg))
	assert.Equal(t, instrum.APIVersion, cfg.APIVersion)
	assert.Equal(t, "prod-1", cfg.Metadata.Name)
	require.NotNil(t, cfg.Spec.K8s)
	assert.True(t, cfg.Spec.K8s.CostMetrics)
	assert.True(t, cfg.Spec.K8s.NodeLogs)
}

// TestRunShow_MissingClusterArg verifies that cobra enforces the required positional argument.
func TestRunShow_MissingClusterArg(t *testing.T) {
	// The cobra.ExactArgs(1) constraint is enforced before RunE is invoked.
	// We test via the command itself to confirm the argument validation.
	cmd := cmdinstr.NewShowCommand(nil) // loader is unused since cobra rejects the call first
	cmd.SetArgs([]string{})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	// cobra wraps the args error; it should mention the usage requirement.
	errMsg := err.Error()
	assert.True(t,
		strings.Contains(errMsg, "accepts 1 arg") || strings.Contains(errMsg, "arg(s)"),
		"expected cobra args error, got: %s", errMsg)
}

// TestRunShow_EmptyCluster verifies that show returns a manifest with empty spec (not an error)
// when a cluster has no instrumentation configured (NC-008).
func TestRunShow_EmptyCluster(t *testing.T) {
	ts := &showTestServer{
		getAppResp: `{}`, // no namespaces
		getK8sResp: `{}`, // all false
	}
	srv := ts.start(t)

	opts := &cmdinstr.ShowOpts{}
	opts.IO.OutputFormat = "yaml"

	var out bytes.Buffer
	err := cmdinstr.RunShow(context.Background(), opts, makeShowClient(srv.URL), "unconfigured-cluster", &out)
	require.NoError(t, err, "show must not return an error for unconfigured cluster")

	var cfg instrum.InstrumentationConfig
	require.NoError(t, unmarshalTestYAML(out.Bytes(), &cfg))
	assert.Equal(t, "unconfigured-cluster", cfg.Metadata.Name)
	assert.Nil(t, cfg.Spec.App, "spec.app must be nil for unconfigured cluster")
	assert.Nil(t, cfg.Spec.K8s, "spec.k8s must be nil for unconfigured cluster")
}

// TestRunShow_ManifestHasNoStackSpecificValues verifies NC-003: the manifest must not contain
// datasource URLs, instance IDs, API tokens, or other stack-specific values.
func TestRunShow_ManifestHasNoStackSpecificValues(t *testing.T) {
	ts := &showTestServer{
		getAppResp: `{"cluster":{"name":"prod-1","namespaces":[{"name":"frontend","selection":"included","tracing":true}]}}`,
		getK8sResp: `{"cluster":{"name":"prod-1","costmetrics":true}}`,
	}
	srv := ts.start(t)

	opts := &cmdinstr.ShowOpts{}
	opts.IO.OutputFormat = "yaml"

	var out bytes.Buffer
	err := cmdinstr.RunShow(context.Background(), opts, makeShowClient(srv.URL), "prod-1", &out)
	require.NoError(t, err)

	content := out.String()
	assert.NotContains(t, content, "datasourceURL", "manifest must not contain datasource URLs")
	assert.NotContains(t, content, "instanceID", "manifest must not contain instance IDs")
	assert.NotContains(t, content, "apiToken", "manifest must not contain API tokens")
	assert.NotContains(t, content, "stackID", "manifest must not contain stack IDs")
	assert.NotContains(t, content, "inst-id", "manifest must not contain test instance ID")

	var cfg instrum.InstrumentationConfig
	require.NoError(t, unmarshalTestYAML(out.Bytes(), &cfg))
	assert.Equal(t, instrum.APIVersion, cfg.APIVersion)
	assert.Equal(t, "prod-1", cfg.Metadata.Name)
}
