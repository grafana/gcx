package resources //nolint:testpackage // Exercise the pull command and its provider processor wiring.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPullOmitsIncidentDescriptionFromFile(t *testing.T) {
	const group = "incident.ext.grafana.app"
	const version = "v1alpha1"
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
	agent.ResetForTesting()
	t.Cleanup(agent.ResetForTesting)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/bootdata":
			http.NotFound(w, r)
		case "/api":
			assert.NoError(t, json.NewEncoder(w).Encode(metav1.APIVersions{Versions: []string{}}))
		case "/apis":
			gv := metav1.GroupVersionForDiscovery{GroupVersion: group + "/" + version, Version: version}
			assert.NoError(t, json.NewEncoder(w).Encode(metav1.APIGroupList{Groups: []metav1.APIGroup{{Name: group, Versions: []metav1.GroupVersionForDiscovery{gv}, PreferredVersion: gv}}}))
		case "/apis/" + group + "/" + version:
			assert.NoError(t, json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: group + "/" + version, APIResources: []metav1.APIResource{{Name: "incidents", SingularName: "incident", Namespaced: true, Kind: "Incident", Verbs: metav1.Verbs{"get", "list"}}}}))
		case "/apis/" + group + "/" + version + "/namespaces/default/incidents/inc-123":
			assert.Equal(t, http.MethodGet, r.Method)
			assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"apiVersion": group + "/" + version,
				"kind":       "Incident",
				"metadata":   map[string]any{"name": "inc-123", "namespace": "default"},
				"spec":       map[string]any{"title": "Outage", "description": "Server details"},
			}))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	work := t.TempDir()
	t.Setenv("GRAFANA_SERVER", server.URL)
	t.Setenv("GRAFANA_STACK_ID", "")
	t.Setenv("GRAFANA_ORG_ID", "1")
	configPath := filepath.Join(work, "config.yaml")
	config := fmt.Sprintf("version: 1\ncredentials:\n  keychain: off\nstacks:\n  selected:\n    grafana:\n      server: %s\n      org-id: 1\ncontexts:\n  selected: {stack: selected}\ncurrent-context: selected\n", server.URL)
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o600))
	destination := filepath.Join(work, "resources")
	root := &cobra.Command{Use: "gcx", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(Command())
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"resources", "--config", configPath, "--context", "selected", "pull", "incidents/inc-123", "--path", destination})
	require.NoError(t, root.ExecuteContext(t.Context()))

	file := filepath.Join(destination, "incidents.v1alpha1.incident.ext.grafana.app", "inc-123.json")
	contents, err := os.ReadFile(file)
	require.NoError(t, err)
	var pulled map[string]any
	require.NoError(t, json.Unmarshal(contents, &pulled))
	spec, ok := pulled["spec"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, spec, "description")
	assert.Equal(t, "Outage", spec["title"])
}
