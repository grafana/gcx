package resources //nolint:testpackage // Exercise production command wiring against a real discovery and resource API.

import (
	"bytes"
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

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/resources"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	clusterTestGroup   = "kgatlas.ext.grafana.app"
	clusterTestVersion = "v1alpha1"
	clusterTestGV      = clusterTestGroup + "/" + clusterTestVersion
)

// clusterScopedFixture serves one group with a namespaced kind (KgSchema)
// and a cluster-scoped kind (SystemKgSchema), as Grafana's kgatlas group does.
type clusterScopedFixture struct {
	config *cmdconfig.Options

	mu     sync.Mutex
	gets   []string
	writes []string
}

func (f *clusterScopedFixture) requests() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.gets...), append([]string(nil), f.writes...)
}

func newClusterScopedFixture(t *testing.T) *clusterScopedFixture {
	t.Helper()
	// Register before Setenv so detection is restored after the environment.
	t.Cleanup(agent.ResetForTesting)
	work := t.TempDir()
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", filepath.Join(work, "discovery"))
	t.Setenv("GCX_OPENAPI_CACHE_DIR", filepath.Join(work, "openapi"))
	agent.ResetForTesting()

	systemSchema := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": clusterTestGV, "kind": "SystemKgSchema",
		"metadata": map[string]any{"name": "core", "resourceVersion": "7"},
		"spec":     map[string]any{"title": "Core schema"},
	}}
	userSchema := unstructured.Unstructured{Object: map[string]any{
		"apiVersion": clusterTestGV, "kind": "KgSchema",
		"metadata": map[string]any{"name": "custom", "namespace": "default"},
		"spec":     map[string]any{"title": "Custom schema"},
	}}

	f := &clusterScopedFixture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			f.mu.Lock()
			f.writes = append(f.writes, r.Method+" "+r.URL.Path)
			f.mu.Unlock()
			http.Error(w, "unexpected mutation", http.StatusInternalServerError)
			return
		}
		var result any
		switch r.URL.Path {
		case "/bootdata":
			http.NotFound(w, r)
			return
		case "/api":
			result = metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{}}
		case "/apis":
			gv := metav1.GroupVersionForDiscovery{GroupVersion: clusterTestGV, Version: clusterTestVersion}
			result = metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}, Groups: []metav1.APIGroup{{Name: clusterTestGroup, Versions: []metav1.GroupVersionForDiscovery{gv}, PreferredVersion: gv}}}
		case "/apis/" + clusterTestGV:
			verbs := metav1.Verbs{"get", "list", "create", "update", "delete"}
			result = metav1.APIResourceList{GroupVersion: clusterTestGV, APIResources: []metav1.APIResource{
				{Name: "kgschemas", SingularName: "kgschema", Namespaced: true, Kind: "KgSchema", Verbs: verbs},
				{Name: "systemkgschemas", SingularName: "systemkgschema", Namespaced: false, Kind: "SystemKgSchema", Verbs: verbs},
			}}
		case "/openapi/v3":
			result = map[string]any{"paths": map[string]any{
				"apis/" + clusterTestGV: map[string]any{"serverRelativeURL": "/openapi/v3/apis/" + clusterTestGV},
			}}
		case "/openapi/v3/apis/" + clusterTestGV:
			result = openAPIDocument("KgSchema", "SystemKgSchema")
		case "/apis/" + clusterTestGV + "/systemkgschemas":
			f.recordGet(r.URL.Path)
			result = &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": clusterTestGV, "kind": "SystemKgSchemaList"}, Items: []unstructured.Unstructured{systemSchema}}
		case "/apis/" + clusterTestGV + "/systemkgschemas/core":
			f.recordGet(r.URL.Path)
			result = systemSchema.Object
		case "/apis/" + clusterTestGV + "/namespaces/default/kgschemas":
			f.recordGet(r.URL.Path)
			result = &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": clusterTestGV, "kind": "KgSchemaList"}, Items: []unstructured.Unstructured{userSchema}}
		default:
			// Pull without a selector also reads provider plugin endpoints,
			// and which ones depends on the registered providers. They are
			// not served here. Only unknown Kubernetes API paths are errors.
			if strings.HasPrefix(r.URL.Path, "/apis/") {
				t.Errorf("unexpected GET %s", r.URL.Path)
			}
			http.NotFound(w, r)
			return
		}
		assert.NoError(t, json.NewEncoder(w).Encode(result))
	}))
	t.Cleanup(server.Close)
	t.Setenv("GRAFANA_SERVER", server.URL)
	t.Setenv("GRAFANA_STACK_ID", "")
	t.Setenv("GRAFANA_ORG_ID", "1")
	configFile := filepath.Join(work, "config.yaml")
	cfg := fmt.Sprintf("version: 1\ncredentials:\n  keychain: off\nstacks:\n  selected:\n    grafana:\n      server: %s\n      org-id: 1\ncontexts:\n  selected: {stack: selected}\ncurrent-context: selected\n", server.URL)
	require.NoError(t, os.WriteFile(configFile, []byte(cfg), 0o600))
	f.config = &cmdconfig.Options{ConfigFile: configFile, Context: "selected"}
	return f
}

// openAPIDocument returns an OpenAPI v3 document with an inline spec schema
// for each kind in the test group.
func openAPIDocument(kinds ...string) map[string]any {
	schemas := map[string]any{}
	for _, kind := range kinds {
		schemas[kind] = map[string]any{
			"x-kubernetes-group-version-kind": []any{map[string]any{"group": clusterTestGroup, "version": clusterTestVersion, "kind": kind}},
			"properties": map[string]any{"spec": map[string]any{
				"type":       "object",
				"properties": map[string]any{"title": map[string]any{"type": "string"}},
			}},
		}
	}
	return map[string]any{"components": map[string]any{"schemas": schemas}}
}

func (f *clusterScopedFixture) recordGet(path string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, path)
}

// runClusterScopedCmd sets args on cmd and returns the buffer that receives stdout.
func runClusterScopedCmd(t *testing.T, cmd *cobra.Command, args ...string) *bytes.Buffer {
	t.Helper()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	return &stdout
}

func TestClusterScoped_ListTypesShowsScope(t *testing.T) {
	f := newClusterScopedFixture(t)

	cmd := listTypesCmd(f.config)
	stdout := runClusterScopedCmd(t, cmd, "-o", "wide", "--no-schema")
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	assert.Contains(t, stdout.String(), "SCOPE")
	assert.Regexp(t, `systemkgschemas\s+systemkgschema\s+SystemKgSchema\s+Cluster`, stdout.String())
	assert.Regexp(t, `kgschemas\s+kgschema\s+KgSchema\s+Namespaced`, stdout.String())

	// The default path fetches schemas, which can drop entries, so check it
	// as well as --no-schema.
	for _, args := range [][]string{{"-o", "json"}, {"-o", "json", "--no-schema"}} {
		cmd = listTypesCmd(f.config)
		stdout = runClusterScopedCmd(t, cmd, args...)
		require.NoError(t, cmd.ExecuteContext(t.Context()), args)
		var nested map[string]map[string][]map[string]any
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &nested), args)
		scopes := map[string]any{}
		for _, entry := range nested[clusterTestGroup][clusterTestVersion] {
			plural, ok := entry["plural"].(string)
			require.True(t, ok)
			scopes[plural] = entry["scope"]
			_, hasSchema := entry["schema"]
			assert.Equal(t, len(args) == 2, hasSchema, "schema for %s with %v", plural, args)
		}
		assert.Equal(t, map[string]any{"kgschemas": "Namespaced", "systemkgschemas": "Cluster"}, scopes, args)
	}
}

func TestClusterScoped_GetUsesClusterURL(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selector string
		wantPath string
	}{
		{name: "list", selector: "systemkgschemas", wantPath: "/apis/" + clusterTestGV + "/systemkgschemas"},
		{name: "single", selector: "systemkgschemas/core", wantPath: "/apis/" + clusterTestGV + "/systemkgschemas/core"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newClusterScopedFixture(t)
			cmd := getCmd(f.config)
			stdout := runClusterScopedCmd(t, cmd, tc.selector, "-o", "json")
			require.NoError(t, cmd.ExecuteContext(t.Context()))
			assert.Contains(t, stdout.String(), `"core"`)

			gets, writes := f.requests()
			assert.Equal(t, []string{tc.wantPath}, gets)
			assert.Empty(t, writes)
		})
	}
}

func TestClusterScoped_GetWithoutSelectorListsClusterScopedKinds(t *testing.T) {
	f := newClusterScopedFixture(t)
	cmd := getCmd(f.config)
	// Provider adapters are also read and fail against this server, so
	// ignore their errors; this case checks only the native kinds.
	stdout := runClusterScopedCmd(t, cmd, "-o", "json", "--on-error", "ignore")
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	assert.Contains(t, stdout.String(), `"core"`)
	assert.Contains(t, stdout.String(), `"custom"`)

	gets, writes := f.requests()
	assert.ElementsMatch(t, []string{
		"/apis/" + clusterTestGV + "/systemkgschemas",
		"/apis/" + clusterTestGV + "/namespaces/default/kgschemas",
	}, gets)
	assert.Empty(t, writes)
}

func TestClusterScoped_Pull(t *testing.T) {
	t.Run("selector is refused before reading", func(t *testing.T) {
		f := newClusterScopedFixture(t)
		destination := filepath.Join(t.TempDir(), "resources")
		cmd := pullCmd(f.config)
		runClusterScopedCmd(t, cmd, "systemkgschemas", "--path", destination, "-o", "json")
		assertClusterScopedUsageError(t, cmd.ExecuteContext(t.Context()), "pull")
		require.NoDirExists(t, destination)
		gets, writes := f.requests()
		assert.Empty(t, gets)
		assert.Empty(t, writes)
	})

	t.Run("no selector skips cluster-scoped kinds", func(t *testing.T) {
		f := newClusterScopedFixture(t)
		destination := filepath.Join(t.TempDir(), "resources")
		cmd := pullCmd(f.config)
		// Provider adapters are also pulled and fail against this server,
		// so ignore their errors; this case checks only the native kinds.
		runClusterScopedCmd(t, cmd, "--path", destination, "-o", "json", "--on-error", "ignore")
		require.NoError(t, cmd.ExecuteContext(t.Context()))

		require.FileExists(t, filepath.Join(destination, "kgschemas."+clusterTestVersion+"."+clusterTestGroup, "custom.json"))
		require.NoDirExists(t, filepath.Join(destination, "systemkgschemas."+clusterTestVersion+"."+clusterTestGroup))
		gets, _ := f.requests()
		assert.Equal(t, []string{"/apis/" + clusterTestGV + "/namespaces/default/kgschemas"}, gets)
	})
}

// assertClusterScopedUsageError checks that err refuses operation on a
// cluster-scoped kind as a usage error (exit 2).
func assertClusterScopedUsageError(t *testing.T, err error, operation string) {
	t.Helper()
	var scopeErr *resources.ClusterScopedOperationError
	require.ErrorAs(t, err, &scopeErr)
	assert.Equal(t, operation, scopeErr.Operation)
	detailed := fail.ErrorToDetailedError(err)
	assert.Equal(t, gcxerrors.SummaryInvalidCommandUsage, detailed.Summary)
	require.NotNil(t, detailed.ExitCode)
	assert.Equal(t, gcxerrors.ExitUsageError, *detailed.ExitCode)
}

func TestClusterScoped_WritesRefused(t *testing.T) {
	t.Run("edit refuses before reading or opening the editor", func(t *testing.T) {
		f := newClusterScopedFixture(t)
		marker := filepath.Join(t.TempDir(), "editor-called")
		t.Setenv("EDITOR", "touch "+marker)
		t.Setenv("VISUAL", "")
		cmd := editCmd(f.config)
		runClusterScopedCmd(t, cmd, "systemkgschemas/core")
		assertClusterScopedUsageError(t, cmd.ExecuteContext(t.Context()), "edit")
		require.NoFileExists(t, marker)
		gets, writes := f.requests()
		assert.Empty(t, gets)
		assert.Empty(t, writes)
	})

	t.Run("delete refuses before reading", func(t *testing.T) {
		f := newClusterScopedFixture(t)
		cmd := deleteCmd(f.config)
		runClusterScopedCmd(t, cmd, "systemkgschemas/core", "--yes")
		assertClusterScopedUsageError(t, cmd.ExecuteContext(t.Context()), "delete")
		gets, writes := f.requests()
		assert.Empty(t, gets)
		assert.Empty(t, writes)
	})

	t.Run("push records a failure and sends no write", func(t *testing.T) {
		f := newClusterScopedFixture(t)
		dir := t.TempDir()
		manifest := `{"apiVersion":"` + clusterTestGV + `","kind":"SystemKgSchema","metadata":{"name":"core"},"spec":{"title":"Core schema"}}`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "core.json"), []byte(manifest), 0o600))
		cmd := pushCmd(f.config)
		var stderr bytes.Buffer
		stdout := runClusterScopedCmd(t, cmd, "--path", dir, "-o", "json")
		cmd.SetErr(&stderr)
		err := cmd.ExecuteContext(t.Context())
		require.Error(t, err)
		assert.Contains(t, stdout.String(), "cluster-scoped", "stderr: %s", stderr.String())
		_, writes := f.requests()
		assert.Empty(t, writes)
	})

	t.Run("push --on-error abort exits as a resource failure", func(t *testing.T) {
		f := newClusterScopedFixture(t)
		dir := t.TempDir()
		manifest := `{"apiVersion":"` + clusterTestGV + `","kind":"SystemKgSchema","metadata":{"name":"core"},"spec":{"title":"Core schema"}}`
		require.NoError(t, os.WriteFile(filepath.Join(dir, "core.json"), []byte(manifest), 0o600))
		cmd := pushCmd(f.config)
		runClusterScopedCmd(t, cmd, "--path", dir, "-o", "json", "--on-error", "abort")
		err := cmd.ExecuteContext(t.Context())
		var scopeErr *resources.ClusterScopedOperationError
		require.ErrorAs(t, err, &scopeErr)
		detailed := fail.ErrorToDetailedError(err)
		require.NotNil(t, detailed)
		require.NotNil(t, detailed.ExitCode)
		assert.Equal(t, gcxerrors.ExitPartialFailure, *detailed.ExitCode)
		_, writes := f.requests()
		assert.Empty(t, writes)
	})
}
