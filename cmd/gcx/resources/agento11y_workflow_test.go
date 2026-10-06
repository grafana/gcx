package resources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	cmdresources "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/output"
	_ "github.com/grafana/gcx/internal/providers/agento11y"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const agento11yWorkflowBase = "/api/plugins/grafana-agento11y-app/resources/eval/"

type agento11yWorkflowKind struct {
	plural       string
	kind         string
	endpoint     string
	idField      string
	spec         string
	updateMethod string
}

type agento11yWorkflowRequest struct {
	method string
	path   string
	body   map[string]any
}

type agento11yWorkflowServer struct {
	mu       sync.Mutex
	exists   bool
	requests []agento11yWorkflowRequest
}

func (s *agento11yWorkflowServer) reset(exists bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exists = exists
	s.requests = nil
}

func (s *agento11yWorkflowServer) recorded() []agento11yWorkflowRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

func newAgento11yWorkflowServer(t *testing.T, kind agento11yWorkflowKind, spec map[string]any) (string, *agento11yWorkflowServer) {
	t.Helper()
	state := &agento11yWorkflowServer{exists: true}
	item := maps.Clone(spec)
	item[kind.idField] = "sample"
	item["tenant_id"] = "tenant-1"
	item["created_by"] = "test-user"
	item["created_at"] = "2026-01-01T00:00:00Z"
	base := agento11yWorkflowBase + kind.endpoint

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		defer state.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		assert.Equal(t, "Bearer workflow-token", r.Header.Get("Authorization"))

		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/api":
				_, _ = w.Write([]byte(`{"kind":"APIVersions","apiVersion":"v1","versions":[]}`))
				return
			case "/apis":
				_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[]}`))
				return
			}
		}

		hit := agento11yWorkflowRequest{method: r.Method, path: r.URL.Path}
		if r.Method != http.MethodGet {
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			if err := json.NewDecoder(r.Body).Decode(&hit.body); err != nil {
				t.Errorf("decode %s %s: %v", r.Method, r.URL.Path, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		}
		state.requests = append(state.requests, hit)

		var response any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == base:
			response = map[string]any{"items": []any{item}}
		case r.Method == http.MethodGet && r.URL.Path == base+"/sample":
			if !state.exists {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"message":"not found"}`))
				return
			}
			response = item
		case r.Method == http.MethodPost && r.URL.Path == base:
			response = item
			state.exists = true
			w.WriteHeader(http.StatusCreated)
		case r.Method == kind.updateMethod && r.URL.Path == base+"/sample":
			response = item
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		assert.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	t.Cleanup(server.Close)
	return server.URL, state
}

func runAgento11yResourceWorkflow(t *testing.T, configPath string, args ...string) ([]byte, string) {
	t.Helper()
	root := &cobra.Command{Use: "gcx"}
	root.AddCommand(cmdresources.Command())
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetIn(strings.NewReader(""))
	root.SetArgs(append([]string{"resources", "--config", configPath, "--context", "workflow"}, args...))
	require.NoError(t, root.ExecuteContext(t.Context()), "args: %v\nstdout: %s\nstderr: %s", args, &stdout, &stderr)
	assert.NotContains(t, stdout.String(), "is deprecated")
	for line := range strings.SplitSeq(strings.TrimSpace(stderr.String()), "\n") {
		if line == "" {
			continue
		}
		if agent.IsAgentMode() {
			var diagnostic struct {
				Class   string `json:"class"`
				Summary string `json:"summary"`
			}
			require.NoError(t, json.Unmarshal([]byte(line), &diagnostic))
			assert.Equal(t, "warning", diagnostic.Class)
			assert.Contains(t, diagnostic.Summary, "is deprecated; use")
		} else {
			assert.True(t, strings.HasPrefix(line, "warn: "), line)
		}
	}
	return stdout.Bytes(), stderr.String()
}

func TestAgento11yResourcePullPushWorkflow(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	kinds := []agento11yWorkflowKind{
		{
			plural: "evaluators", kind: "Evaluator", endpoint: "evaluators", idField: "evaluator_id",
			spec:         `{"version":"2026-01-01","kind":"regex","description":"workflow evaluator","config":{"pattern":"safe"},"output_keys":[{"key":"passed","type":"bool"}]}`,
			updateMethod: http.MethodPost,
		},
		{
			plural: "evalrules", kind: "EvalRule", endpoint: "rules", idField: "rule_id",
			spec:         `{"enabled":true,"selector":"all_assistant_generations","sample_rate":0.5,"evaluator_ids":["sample"]}`,
			updateMethod: http.MethodPatch,
		},
		{
			plural: "hookrules", kind: "HookRule", endpoint: "hook-rules", idField: "rule_id",
			spec:         `{"enabled":true,"phase":"preflight","priority":5,"selector":"all","evaluator_ids":["sample"],"action_on_fail":"deny","short_circuit":true,"tool_filter":{"blocked_names":["exec"]}}`,
			updateMethod: http.MethodPut,
		},
		{
			plural: "collections", kind: "Collection", endpoint: "collections", idField: "collection_id",
			spec:         `{"name":"Workflow collection","description":"Saved conversations for evaluation"}`,
			updateMethod: http.MethodPatch,
		},
	}
	selectors := []struct {
		name   string
		suffix string
		named  bool
	}{
		{name: "bare"},
		{name: "agento11y-short", suffix: ".agento11y"},
		{name: "sigil-short", suffix: ".sigil"},
		{name: "agento11y-full", suffix: ".v1alpha1.agento11y.ext.grafana.app", named: true},
		{name: "sigil-full", suffix: ".v1alpha1.sigil.ext.grafana.app", named: true},
	}

	for _, kind := range kinds {
		t.Run(kind.plural, func(t *testing.T) {
			t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
			var spec map[string]any
			require.NoError(t, json.Unmarshal([]byte(kind.spec), &spec))
			serverURL, server := newAgento11yWorkflowServer(t, kind, spec)
			configPath := filepath.Join(t.TempDir(), "config.yaml")
			config := fmt.Sprintf(`version: 1
stacks:
  workflow:
    grafana:
      server: %s
      token: workflow-token
      stack-id: 12345
contexts:
  workflow:
    stack: workflow
current-context: workflow
`, serverURL)
			require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))

			for _, pullSelector := range selectors {
				for _, format := range []string{"json", "yaml"} {
					t.Run(pullSelector.name+"/"+format, func(t *testing.T) {
						testutils.SetAgentMode(t, format == "yaml")
						server.reset(true)
						path := t.TempDir()
						selector := kind.plural + pullSelector.suffix
						getPath := agento11yWorkflowBase + kind.endpoint
						if pullSelector.named {
							selector += "/sample"
							getPath += "/sample"
						}
						_, stderr := runAgento11yResourceWorkflow(t, configPath, "pull", selector, "-p", path, "-o", format)
						assert.Equal(t, strings.Contains(pullSelector.suffix, "sigil"), strings.Contains(stderr, "is deprecated"), stderr)
						assert.Equal(t, []agento11yWorkflowRequest{{method: http.MethodGet, path: getPath}}, server.recorded())

						filename := filepath.Join(path, kind.plural+".v1alpha1.agento11y.ext.grafana.app", "sample."+format)
						files, err := filepath.Glob(filepath.Join(path, "*", "*"))
						require.NoError(t, err)
						assert.Equal(t, []string{filename}, files)
						contents, err := os.ReadFile(filename)
						require.NoError(t, err)
						var manifest map[string]any
						require.NoError(t, yaml.Unmarshal(contents, &manifest))
						assert.Equal(t, "agento11y.ext.grafana.app/v1alpha1", manifest["apiVersion"])
						assert.Equal(t, kind.kind, manifest["kind"])
						metadata, ok := manifest["metadata"].(map[string]any)
						require.True(t, ok)
						assert.Equal(t, "sample", metadata["name"])
						assert.Equal(t, "stacks-12345", metadata["namespace"])
						assert.Equal(t, spec, manifest["spec"])

						for _, group := range []string{"agento11y", "sigil"} {
							manifestContents := bytes.ReplaceAll(contents, []byte("agento11y.ext.grafana.app/v1alpha1"), []byte(group+".ext.grafana.app/v1alpha1"))
							require.NoError(t, os.WriteFile(filename, manifestContents, 0o600))
							for _, pushSelector := range selectors {
								for _, operation := range []string{"create", "update"} {
									t.Run(group+"-manifest/"+pushSelector.name+"/"+operation, func(t *testing.T) {
										server.reset(operation == "update")
										selector := kind.plural + pushSelector.suffix
										if pushSelector.named {
											selector += "/sample"
										}
										stdout, stderr := runAgento11yResourceWorkflow(t, configPath, "push", selector, "-p", path, "-o", "json")
										assert.Equal(t, strings.Contains(pushSelector.suffix, "sigil"), strings.Contains(stderr, "resource group"), stderr)
										assert.Equal(t, group == "sigil", strings.Contains(stderr, "apiVersion"), stderr)
										var result output.BatchMutation
										require.NoError(t, json.Unmarshal(stdout, &result))
										assert.Equal(t, output.MutationSummary{Succeeded: 1}, result.Summary)
										assert.Empty(t, result.Failures)

										method := kind.updateMethod
										mutationPath := agento11yWorkflowBase + kind.endpoint
										if operation == "create" {
											method = http.MethodPost
										}
										if method != http.MethodPost {
											mutationPath += "/sample"
										}
										body := maps.Clone(spec)
										if method != http.MethodPatch {
											body[kind.idField] = "sample"
										}
										assert.Equal(t, []agento11yWorkflowRequest{
											{method: http.MethodGet, path: agento11yWorkflowBase + kind.endpoint + "/sample"},
											{method: method, path: mutationPath, body: body},
										}, server.recorded())
									})
								}
							}
						}
					})
				}
			}
		})
	}
}
