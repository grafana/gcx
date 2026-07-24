package root_test

// Regression tests for issue #1048: explicit --config must reach the direct
// `gcx appo11y` commands (services, overrides, settings) — the same defect
// class as #951/aio11y, with two extra config-backed surfaces to guard:
// datasource resolution (the query body's datasource UID and the /series
// path both come from the selected config) and the best-effort datasource
// UID save-back (which must write to the selected config file, not the
// default one).
//
// The harness mirrors aio11y_config_routing_test.go (same package): the real
// root command tree via root.NewCommandForTest, two recording servers A and B
// behind config files with distinct URLs, bearer tokens, and stack IDs. The
// stack IDs make routing observable in the services query path itself
// (/apis/query.grafana.app/v0alpha1/namespaces/stacks-<id>/query), and the
// per-config datasource UIDs make it observable in the query body.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/appo11y"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const appO11yProxyBase = "/api/plugin-proxy/grafana-app-observability-app"

// cannedQueryFrames answers every unified-query POST with one instant-vector
// sample whose labels form a superset of what the services parsers read:
// job/telemetry_sdk_language (target_info discovery and metadata),
// client/server (service-graph edges for map and the uninstrumented diff),
// and span_name (list-operations rows). Value 1 keeps HasTraffic true so
// every services verb exits 0.
const cannedQueryFrames = `{"results":{"A":{"frames":[{"schema":{"refId":"A","fields":[{"name":"Time","type":"time"},{"name":"Value","type":"number","labels":{"job":"payments/checkout","telemetry_sdk_language":"go","client":"frontend","server":"checkout","span_name":"HTTP GET","deployment_environment":"prod"}}]},"data":{"values":[[1700000000000],[1]]}}]}}}`

// cannedSeries answers the Prometheus /series proxy route used by
// `services list-labels` (a different endpoint family than the unified
// query API — the datasource UID is part of the URL path).
const cannedSeries = `{"status":"success","data":[{"__name__":"traces_span_metrics_calls_total","job":"payments/checkout","k8s_cluster_name":"prod-us"}]}`

// queryCapture accumulates unified-query POST bodies so tests can assert the
// datasource UID inside the request payload, not just the URL routing.
type queryCapture struct {
	mu     sync.Mutex
	bodies []string
}

func (q *queryCapture) add(b string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.bodies = append(q.bodies, b)
}

func (q *queryCapture) all() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.bodies...)
}

func (q *queryCapture) reset() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.bodies = nil
}

// newAppO11yServer starts a recording server that answers every API family
// the direct appo11y tree touches: the unified datasource query API (canned
// frame), the datasource /series proxy (canned series), /api/datasources
// (caller-supplied list, for the auto-discovery tests), and the
// App Observability plugin-proxy endpoints for overrides and settings.
func newAppO11yServer(t *testing.T, datasourcesJSON string) (*httptest.Server, *recorder, *queryCapture) {
	t.Helper()
	rec := &recorder{}
	qc := &queryCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.record(req)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(req.URL.Path, "/apis/query.grafana.app/") && strings.HasSuffix(req.URL.Path, "/query"),
			req.URL.Path == "/api/ds/query":
			body, _ := io.ReadAll(req.Body)
			qc.add(string(body))
			_, _ = w.Write([]byte(cannedQueryFrames))
		case strings.HasPrefix(req.URL.Path, "/api/datasources/uid/") && strings.HasSuffix(req.URL.Path, "/resources/api/v1/series"):
			_, _ = w.Write([]byte(cannedSeries))
		case req.URL.Path == "/api/datasources":
			_, _ = w.Write([]byte(datasourcesJSON))
		case req.URL.Path == appO11yProxyBase+"/overrides":
			if req.Method == http.MethodGet {
				w.Header().Set("ETag", "etag-1")
				_, _ = w.Write([]byte(`{"metrics_generator":{"collection_interval":"60s"}}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
		case req.URL.Path == appO11yProxyBase+"/provisioned-plugin-settings":
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec, qc
}

// appO11yConfig describes one test config file. promUID (when set) becomes
// contexts.default.datasources.prometheus so datasource resolution comes from
// the selected config without touching /api/datasources; slug (when set)
// becomes stacks.main.slug, which is what arms the canonical-name
// auto-discovery persistence path.
type appO11yConfig struct {
	server  string
	token   string
	stackID int64
	promUID string
	slug    string
}

func writeAppO11yConfigFile(t *testing.T, path string, c appO11yConfig) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	slugLine := ""
	if c.slug != "" {
		slugLine = fmt.Sprintf("    slug: %s\n", c.slug)
	}
	dsBlock := ""
	if c.promUID != "" {
		dsBlock = fmt.Sprintf("    datasources:\n      prometheus: %s\n", c.promUID)
	}
	cfg := fmt.Sprintf(`version: 1
stacks:
  main:
%s    grafana:
      server: %s
      token: %s
      stack-id: %d
contexts:
  default:
    stack: main
%scurrent-context: default
`, slugLine, c.server, c.token, c.stackID, dsBlock)
	require.NoError(t, os.WriteFile(path, []byte(cfg), 0o600))
	return path
}

// runAppO11y executes the real root command tree with only the App
// Observability provider mounted. A fresh tree per invocation keeps cobra
// flag state from leaking between cases.
func runAppO11y(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := root.NewCommandForTest("test", []providers.Provider{&appo11y.AppO11yProvider{}})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

const overridesSpecYAML = `apiVersion: appo11y.ext.grafana.app/v1alpha1
kind: Overrides
metadata:
  name: default
  annotations:
    appo11y.ext.grafana.app/etag: etag-1
spec:
  metrics_generator:
    collection_interval: 60s
`

const settingsSpecYAML = `apiVersion: appo11y.ext.grafana.app/v1alpha1
kind: Settings
metadata:
  name: default
spec: {}
`

// TestAppO11y_ExplicitConfigWinsOverDefault is the core #1048 regression: a
// valid config exists at the default location (server A) and the user passes
// --config for server B. Every direct appo11y verb must reach B, carrying
// B's token, and never touch A — before the fix, all of these silently ran
// against A. For the services verbs the selected config is additionally
// observable in the query URL's namespace (stacks-22222) and in the query
// body's datasource UID (prom-b); list-labels carries the UID in the /series
// path instead.
func TestAppO11y_ExplicitConfigWinsOverDefault(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srvA, recA, _ := newAppO11yServer(t, `[]`)
	srvB, recB, qcB := newAppO11yServer(t, `[]`)
	writeAppO11yConfigFile(t, defaultConfigPath(home), appO11yConfig{server: srvA.URL, token: "token-a", stackID: 11111, promUID: "prom-a"})
	cfgB := writeAppO11yConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), appO11yConfig{server: srvB.URL, token: "token-b", stackID: 22222, promUID: "prom-b"})

	overridesFile := writeSpecFile(t, "overrides.yaml", overridesSpecYAML)
	settingsFile := writeSpecFile(t, "settings.yaml", settingsSpecYAML)

	queryPath := "/apis/query.grafana.app/v0alpha1/namespaces/stacks-22222/query"
	seriesPath := "/api/datasources/uid/prom-b/resources/api/v1/series"

	cases := []struct {
		name    string
		args    []string
		method  string
		path    string
		isQuery bool
	}{
		// Flag before the subcommand, matching the exact #951 repro position.
		{"services list, --config first", []string{"--config", cfgB, "appo11y", "services", "list", "-o", "json"}, "POST", queryPath, true},
		{"services get", []string{"appo11y", "services", "get", "payments/checkout", "--metrics-mode", "v3", "-o", "json", "--config", cfgB}, "POST", queryPath, true},
		{"services map", []string{"appo11y", "services", "map", "payments/checkout", "-o", "json", "--config", cfgB}, "POST", queryPath, true},
		{"services list-operations", []string{"appo11y", "services", "list-operations", "payments/checkout", "--metrics-mode", "v3", "-o", "json", "--config", cfgB}, "POST", queryPath, true},
		{"services list-labels", []string{"appo11y", "services", "list-labels", "payments/checkout", "--metrics-mode", "v3", "-o", "json", "--config", cfgB}, "GET", seriesPath, false},
		{"overrides get", []string{"appo11y", "overrides", "get", "-o", "json", "--config", cfgB}, "GET", appO11yProxyBase + "/overrides", false},
		{"overrides update", []string{"appo11y", "overrides", "update", "-f", overridesFile, "-o", "json", "--config", cfgB}, "POST", appO11yProxyBase + "/overrides", false},
		{"settings get", []string{"appo11y", "settings", "get", "-o", "json", "--config", cfgB}, "GET", appO11yProxyBase + "/provisioned-plugin-settings", false},
		{"settings update", []string{"appo11y", "settings", "update", "-f", settingsFile, "-o", "json", "--config", cfgB}, "POST", appO11yProxyBase + "/provisioned-plugin-settings", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recA.reset()
			recB.reset()
			qcB.reset()
			_, stderr, err := runAppO11y(t, tc.args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.True(t, recB.contains(tc.method, tc.path),
				"expected %s %s on --config server B, got %v", tc.method, tc.path, recB.all())
			assert.Empty(t, recB.wrongAuth("Bearer token-b"),
				"every request to --config server B must carry B's token")
			assert.Zero(t, recA.count(), "default-config server A must receive no requests, got %v", recA.all())
			if tc.isQuery {
				bodies := qcB.all()
				require.NotEmpty(t, bodies, "expected unified-query POST bodies on server B")
				for _, body := range bodies {
					assert.Contains(t, body, `"uid":"prom-b"`,
						"every query body must reference the --config datasource UID")
				}
			}
		})
	}
}

// TestAppO11y_ExplicitConfigWinsOverGCXConfigEnv: GCX_CONFIG points at server
// A, --config at server B. The explicit flag has higher precedence. The
// positive control proves GCX_CONFIG is honored at all in this harness, so
// the precedence cases cannot pass vacuously.
func TestAppO11y_ExplicitConfigWinsOverGCXConfigEnv(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	srvA, recA, _ := newAppO11yServer(t, `[]`)
	srvB, recB, _ := newAppO11yServer(t, `[]`)
	cfgA := writeAppO11yConfigFile(t, filepath.Join(t.TempDir(), "a.yaml"), appO11yConfig{server: srvA.URL, token: "token-a", stackID: 11111, promUID: "prom-a"})
	cfgB := writeAppO11yConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), appO11yConfig{server: srvB.URL, token: "token-b", stackID: 22222, promUID: "prom-b"})
	t.Setenv("GCX_CONFIG", cfgA)

	settingsFile := writeSpecFile(t, "settings.yaml", settingsSpecYAML)

	t.Run("positive control: GCX_CONFIG alone routes to A", func(t *testing.T) {
		recA.reset()
		recB.reset()
		_, stderr, err := runAppO11y(t, "appo11y", "services", "list", "-o", "json")
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Positive(t, recA.count(), "expected requests on the GCX_CONFIG server")
		assert.Empty(t, recA.wrongAuth("Bearer token-a"), "GCX_CONFIG requests must carry A's token")
		assert.Zero(t, recB.count(), "server B must receive no requests, got %v", recB.all())
	})

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"read", []string{"appo11y", "services", "list", "-o", "json", "--config", cfgB}},
		{"mutation", []string{"appo11y", "settings", "update", "-f", settingsFile, "-o", "json", "--config", cfgB}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recA.reset()
			recB.reset()
			_, stderr, err := runAppO11y(t, tc.args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Positive(t, recB.count(), "expected requests on --config server B")
			assert.Empty(t, recB.wrongAuth("Bearer token-b"), "requests must carry B's token")
			assert.Zero(t, recA.count(), "GCX_CONFIG server A must receive no requests, got %v", recA.all())
		})
	}
}

// TestAppO11y_ExplicitConfigWithoutDefault reproduces the original error
// path: no config exists at the default location, and --config alone must be
// sufficient. It also asserts the run does not silently materialize a config
// file at the default location.
func TestAppO11y_ExplicitConfigWithoutDefault(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srvB, recB, _ := newAppO11yServer(t, `[]`)
	cfgB := writeAppO11yConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), appO11yConfig{server: srvB.URL, token: "token-b", stackID: 22222, promUID: "prom-b"})

	overridesFile := writeSpecFile(t, "overrides.yaml", overridesSpecYAML)
	settingsFile := writeSpecFile(t, "settings.yaml", settingsSpecYAML)

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"services list", []string{"appo11y", "services", "list", "-o", "json", "--config", cfgB}},
		{"overrides update", []string{"appo11y", "overrides", "update", "-f", overridesFile, "-o", "json", "--config", cfgB}},
		{"settings update", []string{"appo11y", "settings", "update", "-f", settingsFile, "-o", "json", "--config", cfgB}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recB.reset()
			_, stderr, err := runAppO11y(t, tc.args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Positive(t, recB.count(), "expected requests on --config server B")
			assert.Empty(t, recB.wrongAuth("Bearer token-b"),
				"every request to --config server B must carry B's token")
			_, statErr := os.Stat(defaultConfigPath(home))
			assert.True(t, os.IsNotExist(statErr),
				"no config file may be silently created at the default location")
		})
	}
}

// TestAppO11y_OverridesGetEnvelopeNamespace covers the output side of the
// defect: the K8s-style envelope emitted by `overrides get -o json` embeds
// the namespace derived from the loaded config's stack ID. Routing-only
// assertions cannot catch a mixed path where the request routes correctly
// but the envelope is built from the default config; the namespace can.
func TestAppO11y_OverridesGetEnvelopeNamespace(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srvA, recA, _ := newAppO11yServer(t, `[]`)
	srvB, _, _ := newAppO11yServer(t, `[]`)
	writeAppO11yConfigFile(t, defaultConfigPath(home), appO11yConfig{server: srvA.URL, token: "token-a", stackID: 11111})
	cfgB := writeAppO11yConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), appO11yConfig{server: srvB.URL, token: "token-b", stackID: 22222})

	stdout, stderr, err := runAppO11y(t, "appo11y", "overrides", "get", "-o", "json", "--config", cfgB)
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Zero(t, recA.count(), "default-config server A must receive no requests, got %v", recA.all())

	var envelope struct {
		Metadata struct {
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout: %s", stdout)
	assert.Equal(t, "stacks-22222", envelope.Metadata.Namespace,
		"envelope namespace must come from the --config stack, not the default config")
}

// TestAppO11y_DatasourceDiscoverySaveBack exercises the config-backed
// discovery and cache surface: with no datasource UID configured, resolution
// falls back to /api/datasources on the selected server; with multiple
// matches and a configured stack slug, the canonical Grafana Cloud
// datasource wins and its UID is persisted — into the explicitly selected
// config file, never the default one.
func TestAppO11y_DatasourceDiscoverySaveBack(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	// Two prometheus datasources force the canonical stack-slug match (a
	// single match would resolve without arming persistence).
	dsListB := `[{"uid":"ds-canonical-b","name":"grafanacloud-stack-b-prom","type":"prometheus"},{"uid":"ds-other","name":"other-prom","type":"prometheus"}]`
	srvA, recA, _ := newAppO11yServer(t, `[]`)
	srvB, recB, qcB := newAppO11yServer(t, dsListB)
	cfgAPath := writeAppO11yConfigFile(t, defaultConfigPath(home), appO11yConfig{server: srvA.URL, token: "token-a", stackID: 11111})
	cfgB := writeAppO11yConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), appO11yConfig{server: srvB.URL, token: "token-b", stackID: 22222, slug: "stack-b"})

	cfgABefore, err := os.ReadFile(cfgAPath)
	require.NoError(t, err)

	_, stderr, runErr := runAppO11y(t, "appo11y", "services", "list", "-o", "json", "--config", cfgB)
	require.NoError(t, runErr, "stderr: %s", stderr)

	assert.True(t, recB.contains("GET", "/api/datasources"),
		"datasource auto-discovery must query the --config server, got %v", recB.all())
	assert.True(t, recB.contains("POST", "/apis/query.grafana.app/v0alpha1/namespaces/stacks-22222/query"),
		"the discovered datasource must be queried on the --config server, got %v", recB.all())
	assert.Empty(t, recB.wrongAuth("Bearer token-b"), "discovery must carry B's token")
	assert.Zero(t, recA.count(), "default-config server A must receive no requests, got %v", recA.all())
	bodies := qcB.all()
	require.NotEmpty(t, bodies, "expected unified-query POST bodies on server B")
	for _, body := range bodies {
		assert.Contains(t, body, `"uid":"ds-canonical-b"`,
			"queries must use the canonical datasource discovered via the --config stack slug")
	}

	cfgBAfter, err := os.ReadFile(cfgB)
	require.NoError(t, err)
	assert.Contains(t, string(cfgBAfter), "ds-canonical-b",
		"the discovered UID must be persisted into the --config file")

	cfgAAfter, err := os.ReadFile(cfgAPath)
	require.NoError(t, err)
	assert.Equal(t, string(cfgABefore), string(cfgAAfter),
		"the default config file must not be modified")
}

// TestAppO11y_ContextFlagSmoke guards what #1012 fixed: --context selects
// between contexts of the discovered default config, credentials included.
func TestAppO11y_ContextFlagSmoke(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srvA, recA, _ := newAppO11yServer(t, `[]`)
	srvB, recB, _ := newAppO11yServer(t, `[]`)
	path := defaultConfigPath(home)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	cfg := fmt.Sprintf(`version: 1
stacks:
  stack-a:
    grafana:
      server: %s
      token: token-a
      stack-id: 11111
  stack-b:
    grafana:
      server: %s
      token: token-b
      stack-id: 22222
contexts:
  a:
    stack: stack-a
  b:
    stack: stack-b
current-context: a
`, srvA.URL, srvB.URL)
	require.NoError(t, os.WriteFile(path, []byte(cfg), 0o600))

	_, stderr, err := runAppO11y(t, "--context", "b", "appo11y", "overrides", "get", "-o", "json")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Positive(t, recB.count(), "expected requests on context b's server")
	assert.Empty(t, recB.wrongAuth("Bearer token-b"), "requests must carry context b's token")
	assert.Zero(t, recA.count(), "context a's server must receive no requests, got %v", recA.all())
}

// TestAppO11y_EnvVarSmoke guards the env-override path: GRAFANA_SERVER and
// GRAFANA_TOKEN override an existing config file, credentials included. (The
// env-only path with no config file at all is tracked separately in #1049.)
func TestAppO11y_EnvVarSmoke(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srvA, recA, _ := newAppO11yServer(t, `[]`)
	srvB, recB, _ := newAppO11yServer(t, `[]`)
	writeAppO11yConfigFile(t, defaultConfigPath(home), appO11yConfig{server: srvA.URL, token: "token-a", stackID: 11111})
	t.Setenv("GRAFANA_SERVER", srvB.URL)
	t.Setenv("GRAFANA_TOKEN", "env-token")

	_, stderr, err := runAppO11y(t, "appo11y", "overrides", "get", "-o", "json")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Positive(t, recB.count(), "expected requests on the env-var server")
	assert.Empty(t, recB.wrongAuth("Bearer env-token"), "requests must carry the env token, not the config file's")
	assert.Zero(t, recA.count(), "config-file server A must receive no requests, got %v", recA.all())
}
