package root_test

// Regression tests for issue #951: explicit --config must reach the direct
// `gcx agento11y` CRUD commands (evaluators, rules, guards, collections).
//
// Each case builds the real root command tree (root.NewCommandForTest) with
// only the Agent Observability provider mounted, points two recording HTTP
// servers A and B at distinct config files, and asserts every plugin-API
// request lands on the server the user selected — and none on the other.
// Every fixed verb gets its own routing case because each subcommand
// constructor captures the loader independently: a verb-local regression
// cannot be caught by a neighboring verb's case.
//
// The two configs also differ in stack-id and token, so two same-shaped
// mixed-path defect classes stay observable: distinct stack-ids give each
// config a deterministic namespace (stacks-11111 vs stacks-22222, no
// /bootdata round-trip) for the collections-update envelope assertion, and
// distinct tokens let the recorder assert requests carry the selected
// config's credentials, not the other config's or the environment's.

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
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hit struct {
	method string
	path   string
	auth   string
}

func (h hit) String() string { return h.method + " " + h.path + " [" + h.auth + "]" }

// recorder captures every request that reaches a test server, including the
// Authorization header so credential routing is as observable as URL routing.
type recorder struct {
	mu   sync.Mutex
	hits []hit
}

func (r *recorder) record(req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits = append(r.hits, hit{method: req.Method, path: req.URL.Path, auth: req.Header.Get("Authorization")})
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hits)
}

func (r *recorder) all() []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.hits)
}

func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hits = nil
}

func (r *recorder) contains(method, path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.ContainsFunc(r.hits, func(h hit) bool { return h.method == method && h.path == path })
}

// wrongAuth returns the hits whose Authorization header differs from want.
func (r *recorder) wrongAuth(want string) []hit {
	r.mu.Lock()
	defer r.mu.Unlock()
	var bad []hit
	for _, h := range r.hits {
		if h.auth != want {
			bad = append(bad, h)
		}
	}
	return bad
}

// newPluginServer starts a recording server that answers the grafana-sigil-app
// plugin API well enough for every CRUD command to succeed: lists and gets
// return an empty page/object, deletes return {}, and mutations return one
// canned object whose keys satisfy all four definition decoders (unknown
// fields are ignored).
func newPluginServer(t *testing.T) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rec.record(req)
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

func writeConfigFile(t *testing.T, path, server, token string, stackID int64) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	cfg := fmt.Sprintf(`version: 1
stacks:
  main:
    grafana:
      server: %s
      token: %s
      stack-id: %d
contexts:
  default:
    stack: main
current-context: default
`, server, token, stackID)
	require.NoError(t, os.WriteFile(path, []byte(cfg), 0o600))
	return path
}

func defaultConfigPath(home string) string {
	return filepath.Join(home, ".config", "gcx", "config.yaml")
}

// runAIO11y executes the real root command tree with only the Agent
// Observability provider mounted. A fresh tree per invocation keeps cobra
// flag state from leaking between cases.
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
// --config for server B. Every fixed verb must reach B, carrying B's token,
// and never touch A — before the fix, all of these silently ran against A.
// Collections update has its own dedicated test below (envelope namespace).
func TestAIO11y_ExplicitConfigWinsOverDefault(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
	writeConfigFile(t, defaultConfigPath(home), srvA.URL, "token-a", 11111)
	cfgB := writeConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), srvB.URL, "token-b", 22222)

	evaluatorFile := writeSpecFile(t, "evaluator.yaml", "evaluator_id: e-1\nkind: llm_judge\n")
	ruleFile := writeSpecFile(t, "rule.yaml", "rule_id: r-1\nenabled: true\n")
	guardFile := writeSpecFile(t, "guard.yaml", "rule_id: g-1\nphase: preflight\n")

	cases := []struct {
		name   string
		args   []string
		method string
		path   string
	}{
		// Flag before the subcommand, matching the exact #951 repro position.
		{"evaluators list, --config first", []string{"--config", cfgB, "agento11y", "evaluators", "list", "-o", "json"}, "GET", "/eval/evaluators"},
		{"evaluators get", []string{"agento11y", "evaluators", "get", "e-1", "-o", "json", "--config", cfgB}, "GET", "/eval/evaluators/e-1"},
		{"evaluators upsert", []string{"agento11y", "evaluators", "upsert", "-f", evaluatorFile, "-o", "json", "--config", cfgB}, "POST", "/eval/evaluators"},
		{"evaluators delete", []string{"agento11y", "evaluators", "delete", "e-1", "--force", "--config", cfgB}, "DELETE", "/eval/evaluators/e-1"},
		{"rules list", []string{"agento11y", "rules", "list", "-o", "json", "--config", cfgB}, "GET", "/eval/rules"},
		{"rules get", []string{"agento11y", "rules", "get", "r-1", "-o", "json", "--config", cfgB}, "GET", "/eval/rules/r-1"},
		{"rules create", []string{"agento11y", "rules", "create", "-f", ruleFile, "-o", "json", "--config", cfgB}, "POST", "/eval/rules"},
		{"rules update", []string{"agento11y", "rules", "update", "r-1", "-f", ruleFile, "-o", "json", "--config", cfgB}, "PATCH", "/eval/rules/r-1"},
		{"rules delete", []string{"agento11y", "rules", "delete", "r-1", "--force", "--config", cfgB}, "DELETE", "/eval/rules/r-1"},
		{"guards list", []string{"agento11y", "guards", "list", "-o", "json", "--config", cfgB}, "GET", "/eval/hook-rules"},
		{"guards get", []string{"agento11y", "guards", "get", "g-1", "-o", "json", "--config", cfgB}, "GET", "/eval/hook-rules/g-1"},
		{"guards create", []string{"agento11y", "guards", "create", "-f", guardFile, "-o", "json", "--config", cfgB}, "POST", "/eval/hook-rules"},
		{"guards update", []string{"agento11y", "guards", "update", "g-1", "-f", guardFile, "-o", "json", "--config", cfgB}, "PUT", "/eval/hook-rules/g-1"},
		{"guards delete", []string{"agento11y", "guards", "delete", "g-1", "--force", "--config", cfgB}, "DELETE", "/eval/hook-rules/g-1"},
		{"collections list", []string{"agento11y", "collections", "list", "-o", "json", "--config", cfgB}, "GET", "/eval/collections"},
		{"collections get", []string{"agento11y", "collections", "get", "c-1", "-o", "json", "--config", cfgB}, "GET", "/eval/collections/c-1"},
		{"collections create", []string{"agento11y", "collections", "create", "--name", "suite", "-o", "json", "--config", cfgB}, "POST", "/eval/collections"},
		{"collections delete", []string{"agento11y", "collections", "delete", "c-1", "--force", "--config", cfgB}, "DELETE", "/eval/collections/c-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recA.reset()
			recB.reset()
			_, stderr, err := runAIO11y(t, tc.args...)
			require.NoError(t, err, "stderr: %s", stderr)
			assert.True(t, recB.contains(tc.method, pluginBase+tc.path),
				"expected %s %s on --config server B, got %v", tc.method, pluginBase+tc.path, recB.all())
			assert.Empty(t, recB.wrongAuth("Bearer token-b"),
				"every request to --config server B must carry B's token")
			assert.Zero(t, recA.count(), "default-config server A must receive no requests, got %v", recA.all())
		})
	}
}

// TestAIO11y_ExplicitConfigWinsOverGCXConfigEnv: GCX_CONFIG points at server A,
// --config at server B. The explicit flag has higher precedence. The positive
// control proves GCX_CONFIG is honored at all in this harness, so the
// precedence cases cannot pass vacuously.
func TestAIO11y_ExplicitConfigWinsOverGCXConfigEnv(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
	cfgA := writeConfigFile(t, filepath.Join(t.TempDir(), "a.yaml"), srvA.URL, "token-a", 11111)
	cfgB := writeConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), srvB.URL, "token-b", 22222)
	t.Setenv("GCX_CONFIG", cfgA)

	ruleFile := writeSpecFile(t, "rule.yaml", "rule_id: r-1\nenabled: true\n")

	t.Run("positive control: GCX_CONFIG alone routes to A", func(t *testing.T) {
		recA.reset()
		recB.reset()
		_, stderr, err := runAIO11y(t, "agento11y", "evaluators", "list", "-o", "json")
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Positive(t, recA.count(), "expected requests on the GCX_CONFIG server")
		assert.Empty(t, recA.wrongAuth("Bearer token-a"), "GCX_CONFIG requests must carry A's token")
		assert.Zero(t, recB.count(), "server B must receive no requests, got %v", recB.all())
	})

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
			assert.Empty(t, recB.wrongAuth("Bearer token-b"), "requests must carry B's token")
			assert.Zero(t, recA.count(), "GCX_CONFIG server A must receive no requests, got %v", recA.all())
		})
	}
}

// TestAIO11y_ExplicitConfigWithoutDefault reproduces the original #951 error
// path: no config exists at the default location, and --config alone must be
// sufficient. Before the fix these commands failed with "Invalid
// configuration" from the auto-created empty default config.
func TestAIO11y_ExplicitConfigWithoutDefault(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	srvB, recB := newPluginServer(t)
	cfgB := writeConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), srvB.URL, "token-b", 22222)

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
	home := testutils.SandboxConfigEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
	writeConfigFile(t, defaultConfigPath(home), srvA.URL, "token-a", 11111)
	cfgB := writeConfigFile(t, filepath.Join(t.TempDir(), "b.yaml"), srvB.URL, "token-b", 22222)

	stdout, stderr, err := runAIO11y(t, "agento11y", "collections", "update", "c-1", "--name", "renamed", "-o", "json", "--config", cfgB)
	require.NoError(t, err, "stderr: %s", stderr)

	assert.True(t, recB.contains(http.MethodPatch, pluginBase+"/eval/collections/c-1"),
		"expected PATCH on --config server B, got %v", recB.all())
	assert.Empty(t, recB.wrongAuth("Bearer token-b"), "the PATCH must carry B's token")
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
// between contexts of the discovered default config, credentials included.
func TestAIO11y_ContextFlagSmoke(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
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
	assert.Empty(t, recB.wrongAuth("Bearer token-b"), "requests must carry context b's token")
	assert.Zero(t, recA.count(), "context a's server must receive no requests, got %v", recA.all())
}

// TestAIO11y_EnvVarSmoke guards the env-override path that always worked for
// #951's reporter: GRAFANA_SERVER/GRAFANA_TOKEN override an existing config
// file, credentials included. (The env-only path with no config file at all
// is broken on main for every ConfigLoader-based provider — tracked
// separately in #1049 — and is out of scope here.)
func TestAIO11y_EnvVarSmoke(t *testing.T) {
	home := testutils.SandboxConfigEnv(t)
	srvA, recA := newPluginServer(t)
	srvB, recB := newPluginServer(t)
	writeConfigFile(t, defaultConfigPath(home), srvA.URL, "token-a", 11111)
	t.Setenv("GRAFANA_SERVER", srvB.URL)
	t.Setenv("GRAFANA_TOKEN", "env-token")

	_, stderr, err := runAIO11y(t, "agento11y", "evaluators", "list", "-o", "json")
	require.NoError(t, err, "stderr: %s", stderr)
	assert.Positive(t, recB.count(), "expected requests on the env-var server")
	assert.Empty(t, recB.wrongAuth("Bearer env-token"), "requests must carry the env token, not the config file's")
	assert.Zero(t, recA.count(), "config-file server A must receive no requests, got %v", recA.all())
}
