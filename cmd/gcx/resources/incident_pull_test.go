package resources //nolint:testpackage // Exercise the pull command and its registration processor wiring.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/resources/adapter"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPullOmitsRegisteredFieldsFromFile(t *testing.T) {
	// Provider registration lasts for the process. Keep it out of other command tests.
	if os.Getenv("GCX_PULL_STRIP_TEST_CHILD") != "1" {
		executable, err := os.Executable()
		require.NoError(t, err)
		cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestPullOmitsRegisteredFieldsFromFile$") // #nosec G204 -- Run this test executable with fixed arguments.
		cmd.Env = append(os.Environ(), "GCX_PULL_STRIP_TEST_CHILD=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", string(output))
		return
	}
	const group = "pull-strip.test.example.com"
	const version = "v1alpha1"
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
	providers.Register(adapter.NewProvider("pull-strip-test", "Pull strip test", func(context.Context) (adapter.ClientDeps, error) { return adapter.ClientDeps{}, nil },
		adapter.Resource[pullStripExample]{Group: group, Version: version, Kind: "Example", PullStripFields: []string{"readOnlyDetail"},
			NewClient: func(context.Context, adapter.ClientDeps) (any, error) { return pullStripClient{}, nil },
		}))
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
			assert.NoError(t, json.NewEncoder(w).Encode(metav1.APIResourceList{GroupVersion: group + "/" + version, APIResources: []metav1.APIResource{{Name: "examples", SingularName: "example", Namespaced: true, Kind: "Example", Verbs: metav1.Verbs{"get", "list"}}}}))
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
	root.SetArgs([]string{"resources", "--config", configPath, "--context", "selected", "pull", "examples/inc-123", "--path", destination})
	require.NoError(t, root.ExecuteContext(t.Context()))

	file := filepath.Join(destination, "examples.v1alpha1.pull-strip.test.example.com", "inc-123.json")
	contents, err := os.ReadFile(file)
	require.NoError(t, err)
	var pulled map[string]any
	require.NoError(t, json.Unmarshal(contents, &pulled))
	spec, ok := pulled["spec"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, spec, "readOnlyDetail")
	assert.Equal(t, "Outage", spec["title"])
}

// pullStripExample supplies a provider-specific field to the generic pull test.
//
//nolint:recvcheck // Value naming and pointer assignment satisfy the adapter contract.
type pullStripExample struct {
	Name           string `json:"name"`
	Title          string `json:"title"`
	ReadOnlyDetail string `json:"readOnlyDetail"`
}

func (e pullStripExample) GetResourceName() string      { return e.Name }
func (e *pullStripExample) SetResourceName(name string) { e.Name = name }

type pullStripClient struct{}

func (pullStripClient) Get(context.Context, string) (*pullStripExample, error) {
	return &pullStripExample{Name: "inc-123", Title: "Outage", ReadOnlyDetail: "Server details"}, nil
}
