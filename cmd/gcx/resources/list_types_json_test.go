package resources_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	cmdresources "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListTypes_JSONFieldSelectsPerDescriptor pins that
// `gcx resources list-types --json kind` selects per descriptor. The command
// once passed a plain map envelope, which put selection on the whole object,
// so every field that --json list advertises looked absent.
func TestListTypes_JSONFieldSelectsPerDescriptor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			_, _ = w.Write([]byte(`{"kind":"APIVersions","apiVersion":"v1","versions":[]}`))
		case "/apis":
			_, _ = w.Write([]byte(`{"kind":"APIGroupList","apiVersion":"v1","groups":[{` +
				`"name":"dashboard.grafana.app",` +
				`"versions":[{"groupVersion":"dashboard.grafana.app/v1beta1","version":"v1beta1"}],` +
				`"preferredVersion":{"groupVersion":"dashboard.grafana.app/v1beta1","version":"v1beta1"}}]}`))
		case "/apis/dashboard.grafana.app/v1beta1":
			_, _ = w.Write([]byte(`{"kind":"APIResourceList","apiVersion":"v1",` +
				`"groupVersion":"dashboard.grafana.app/v1beta1","resources":[{` +
				`"name":"dashboards","singularName":"dashboard","namespaced":true,` +
				`"kind":"Dashboard","verbs":["get","list"]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	configFile := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(configFile, []byte(`version: 1
stacks:
  test:
    grafana:
      server: `+srv.URL+`
      token: test-token
      org-id: 1
      auth-method: token
contexts:
  test:
    stack: test
current-context: test
`), 0o600))

	root := &cobra.Command{Use: "gcx", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(cmdresources.Command())

	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"resources", "--config", configFile, "list-types", "--json", "kind"})
	require.NoError(t, root.Execute(), "stderr: %s", stderr.String())

	var got struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &got), "stdout: %s", stdout.String())
	require.NotEmpty(t, got.Items, "stdout: %s", stdout.String())

	kinds := make([]string, 0, len(got.Items))
	for _, item := range got.Items {
		assert.Len(t, item, 1, "only the requested path may appear")
		kind, ok := item["kind"].(string)
		require.True(t, ok, "item %v carries no kind", item)
		kinds = append(kinds, kind)
	}
	assert.Contains(t, kinds, "Dashboard")
}
