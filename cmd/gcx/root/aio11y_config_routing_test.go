package root_test

// Regression tests for issue #951: explicit --config must reach the direct
// `gcx agento11y` CRUD commands (evaluators, rules, guards, collections).
//
// Each case builds the real root command tree (root.NewCommandForTest) with
// only the Agent Observability provider mounted, points two recording HTTP
// servers A and B at distinct config files, and asserts every plugin-API
// request lands on the server the user selected — and none on the other.
// Distinct stack-ids give each config a deterministic namespace
// (stacks-11111 vs stacks-22222) without any /bootdata discovery round-trip,
// which also lets the collections-update case assert the output envelope's
// namespace comes from the selected config.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/gcx/cmd/gcx/root"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/aio11y"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recorder captures every request that reaches a test server.
type recorder struct {
	mu   sync.Mutex
	hits []string
}

func (r *recorder) record(method, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits = append(r.hits, method+" "+path)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hits)
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.hits...)
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits = nil
}

func (r *recorder) contains(methodAndPath string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.hits, methodAndPath)
}

// newPluginServer starts a recording server that answers the grafana-sigil-app
// plugin API well enough for every CRUD command to succeed: lists return an
// empty page, deletes return {}, and mutations return one canned object whose
// keys satisfy all four definition decoders (unknown fields are ignored).
func newPluginServer(t *testing.T) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.record(req.Method, req.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"items": []}`))
		case http.MethodDelete:
			_, _ = w.Write([]byte(`{}`))
		default: // POST, PUT, PATCH
			_, _ = w.Write([]byte(`{"evaluator_id":"e-1","rule_id":"r-1","collection_id":"c-1","name":"n","kind":"llm_judge"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// sandboxEnv isolates the test from the developer's real config and
// environment: config discovery is confined to a temp HOME, every GRAFANA_*
// and GCX_CONFIG variable is cleared (t.Setenv registers restoration), agent
// mode is pinned off, and telemetry is disabled.
func sandboxEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_TELEMETRY", "disabled")
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GRAFANA_") || k == "GCX_CONFIG" {
			t.Setenv(k, os.Getenv(k))
			os.Unsetenv(k)
		}
	}
	return home
}

func writeConfigFile(t *testing.T, path, server string, stackID int64) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	cfg := fmt.Sprintf(`version: 1
stacks:
  main:
    grafana:
      server: %s
      token: test-token
      stack-id: %d
contexts:
  default:
    stack: main
current-context: default
`, server, stackID)
	require.NoError(t, os.WriteFile(path, []byte(cfg), 0o600))
	return path
}

func defaultConfigPath(home string) string {
	return filepath.Join(home, ".config", "gcx", "config.yaml")
}

// runAIO11y executes the real root command tree with only the Agent Observability
// provider mounted. A fresh tree per invocation keeps cobra flag state from
// leaking between cases.
func runAIO11y(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := root.NewCommandForTest("test", []providers.Provider{&aio11y.AIO11yProvider{}})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), err
}

func writeSpecFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

const pluginBase = "/api/plugins/grafana-sigil-app/resources"

// TestAIO11y_ExplicitConfigWinsOverDefault is the core #951 regression: a
// valid config exists at the default location (server A) and the user passes
// --config for server B. Every read and mutation must reach B and never A —
// before the fix, all of these silently ran against A.
func TestAIO11y_ExplicitConfigWinsOverDefault(t *testing.T) {
	home := sandboxEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
	writeConfigFile(t, defaultConfigPath(home), srvA.URL, 11111)
	cfgB := writeConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), srvB.URL, 22222)

	evaluatorFile := writeSpecFile(t, "evaluator.yaml", "evaluator_id: e-1\nkind: llm_judge\n")
	ruleFile := writeSpecFile(t, "rule.yaml", "rule_id: r-1\nenabled: true\n")
	guardFile := writeSpecFile(t, "guard.yaml", "rule_id: g-1\nphase: preflight\n")

	cases := []struct {
		name string
		args []string
		hit  string
	}{
		// Flag before the subcommand, matching the exact #951 repro position.
		{"evaluators list, --config first", []string{"--config", cfgB, "agento11y", "evaluators", "list", "-o", "json"}, "GET " + pluginBase + "/eval/evaluators"},
		{"evaluators upsert", []string{"agento11y", "evaluators", "upsert", "-f", evaluatorFile, "-o", "json", "--config", cfgB}, "POST " + pluginBase + "/eval/evaluators"},
		{"evaluators delete", []string{"agento11y", "evaluators", "delete", "e-1", "--force", "--config", cfgB}, "DELETE " + pluginBase + "/eval/evaluators/e-1"},
		{"rules list", []string{"agento11y", "rules", "list", "-o", "json", "--config", cfgB}, "GET " + pluginBase + "/eval/rules"},
		{"rules create", []string{"agento11y", "rules", "create", "-f", ruleFile, "-o", "json", "--config", cfgB}, "POST " + pluginBase + "/eval/rules"},
		{"guards list", []string{"agento11y", "guards", "list", "-o", "json", "--config", cfgB}, "GET " + pluginBase + "/eval/hook-rules"},
		{"guards create", []string{"agento11y", "guards", "create", "-f", guardFile, "-o", "json", "--config", cfgB}, "POST " + pluginBase + "/eval/hook-rules"},
		{"collections list", []string{"agento11y", "collections", "list", "-o", "json", "--config", cfgB}, "GET " + pluginBase + "/eval/collections"},
		{"collections create", []string{"agento11y", "collections", "create", "--name", "suite", "-o", "json", "--config", cfgB}, "POST " + pluginBase + "/eval/collections"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recA.reset()
			recB.reset()
			_, stderr, err := runAIO11y(t, tc.args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.True(t, recB.contains(tc.hit), "expected %q on --config server B, got %v", tc.hit, recB.all())
			assert.Zero(t, recA.count(), "default-config server A must receive no requests, got %v", recA.all())
		})
	}
}

// TestAIO11y_ExplicitConfigWinsOverGCXConfigEnv: GCX_CONFIG points at server A,
// --config at server B. The explicit flag has higher precedence.
func TestAIO11y_ExplicitConfigWinsOverGCXConfigEnv(t *testing.T) {
	sandboxEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
	cfgA := writeConfigFile(t, filepath.Join(t.TempDir(), "a.yaml"), srvA.URL, 11111)
	cfgB := writeConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), srvB.URL, 22222)
	t.Setenv("GCX_CONFIG", cfgA)

	ruleFile := writeSpecFile(t, "rule.yaml", "rule_id: r-1\nenabled: true\n")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"read", []string{"agento11y", "evaluators", "list", "-o", "json", "--config", cfgB}},
		{"mutation", []string{"agento11y", "rules", "create", "-f", ruleFile, "-o", "json", "--config", cfgB}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recA.reset()
			recB.reset()
			_, stderr, err := runAIO11y(t, tc.args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Positive(t, recB.count(), "expected requests on --config server B")
			assert.Zero(t, recA.count(), "GCX_CONFIG server A must receive no requests, got %v", recA.all())
		})
	}
}

// TestAIO11y_ExplicitConfigWithoutDefault reproduces the original #951 error
// path: no config exists at the default location, and --config alone must be
// sufficient. Before the fix these commands failed with "Invalid
// configuration" from the auto-created empty default config.
func TestAIO11y_ExplicitConfigWithoutDefault(t *testing.T) {
	sandboxEnv(t)
	srvB, recB := newPluginServer(t)
	cfgB := writeConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), srvB.URL, 22222)

	guardFile := writeSpecFile(t, "guard.yaml", "rule_id: g-1\nphase: preflight\n")

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"evaluators list", []string{"agento11y", "evaluators", "list", "-o", "json", "--config", cfgB}},
		{"guards create", []string{"agento11y", "guards", "create", "-f", guardFile, "-o", "json", "--config", cfgB}},
		{"collections update", []string{"agento11y", "collections", "update", "c-1", "--name", "renamed", "-o", "json", "--config", cfgB}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recB.reset()
			_, stderr, err := runAIO11y(t, tc.args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.Positive(t, recB.count(), "expected requests on --config server B")
		})
	}
}

// TestAIO11y_CollectionsUpdateEnvelopeNamespace covers the mixed-path defect
// in collections update: the PATCH already routed through the bound loader,
// but the output envelope was built from a TypedCRUD loaded off the default
// config, so its namespace came from the wrong stack. Routing-only assertions
// cannot catch this; the namespace assertion can.
func TestAIO11y_CollectionsUpdateEnvelopeNamespace(t *testing.T) {
	home := sandboxEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
	writeConfigFile(t, defaultConfigPath(home), srvA.URL, 11111)
	cfgB := writeConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), srvB.URL, 22222)

	stdout, stderr, err := runAIO11y(t, "agento11y", "collections", "update", "c-1", "--name", "renamed", "-o", "json", "--config", cfgB)
	require.NoError(t, err, "stderr: %s", stderr)

	assert.True(t, recB.contains("PATCH "+pluginBase+"/eval/collections/c-1"),
		"expected PATCH on --config server B, got %v", recB.all())
	assert.Zero(t, recA.count(), "default-config server A must receive no requests, got %v", recA.all())

	var envelope struct {
		Metadata struct {
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &envelope), "stdout: %s", stdout)
	assert.Equal(t, "stacks-22222", envelope.Metadata.Namespace,
		"output envelope namespace must come from the --config stack, not the default config")
}

// TestAIO11y_ContextFlagSmoke guards what #1012 fixed: --context selects
// between contexts of the discovered default config.
func TestAIO11y_ContextFlagSmoke(t *testing.T) {
	home := sandboxEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
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

	_, stderr, err := runAIO11y(t, "--context", "b", "agento11y", "evaluators", "list", "-o", "json")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Positive(t, recB.count(), "expected requests on context b's server")
	assert.Zero(t, recA.count(), "context a's server must receive no requests, got %v", recA.all())
}

// TestAIO11y_EnvVarSmoke guards the env-override path that always worked for
// #951's reporter: GRAFANA_SERVER/GRAFANA_TOKEN override an existing config
// file. (The env-only path with no config file at all is broken on main for
// every ConfigLoader-based provider — "$.stacks.default.grafana: node not
// found" — which is a separate pre-existing defect, out of scope here.)
func TestAIO11y_EnvVarSmoke(t *testing.T) {
	home := sandboxEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
	writeConfigFile(t, defaultConfigPath(home), srvA.URL, 11111)
	t.Setenv("GRAFANA_SERVER", srvB.URL)
	t.Setenv("GRAFANA_TOKEN", "env-token")

	_, stderr, err := runAIO11y(t, "agento11y", "evaluators", "list", "-o", "json")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Positive(t, recB.count(), "expected requests on the env-var server")
	assert.Zero(t, recA.count(), "config-file server A must receive no requests, got %v", recA.all())
}
