package resources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	cmdresources "github.com/grafana/gcx/cmd/gcx/resources"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeleteNativeSelectorsStillFetchAndDelete(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprintf("named=%t", named), func(t *testing.T) {
			t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
			t.Setenv("GCX_KEYCHAIN", "off")
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
			for _, name := range []string{"GRAFANA_TOKEN", "GRAFANA_USER", "GRAFANA_PASSWORD", "GRAFANA_PROXY_ENDPOINT"} {
				t.Setenv(name, "")
			}

			const groupVersion = "dashboard.grafana.app/v1beta1"
			const resourcePath = "/apis/" + groupVersion + "/namespaces/default/dashboards"
			var reads, deletes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api":
					_ = json.NewEncoder(w).Encode(metav1.APIVersions{TypeMeta: metav1.TypeMeta{Kind: "APIVersions", APIVersion: "v1"}, Versions: []string{}})
				case "/apis":
					version := metav1.GroupVersionForDiscovery{GroupVersion: groupVersion, Version: "v1beta1"}
					_ = json.NewEncoder(w).Encode(metav1.APIGroupList{
						TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"},
						Groups:   []metav1.APIGroup{{Name: "dashboard.grafana.app", Versions: []metav1.GroupVersionForDiscovery{version}, PreferredVersion: version}},
					})
				case "/apis/" + groupVersion:
					_ = json.NewEncoder(w).Encode(metav1.APIResourceList{
						TypeMeta: metav1.TypeMeta{Kind: "APIResourceList", APIVersion: "v1"}, GroupVersion: groupVersion,
						APIResources: []metav1.APIResource{{Name: "dashboards", SingularName: "dashboard", Kind: "Dashboard", Namespaced: true}},
					})
				case resourcePath:
					reads.Add(1)
					_, _ = w.Write([]byte(`{"apiVersion":"dashboard.grafana.app/v1beta1","kind":"DashboardList","items":[]}`))
				case resourcePath + "/sample":
					if r.Method == http.MethodDelete {
						deletes.Add(1)
						_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Success"}`))
						return
					}
					reads.Add(1)
					_, _ = w.Write([]byte(`{"apiVersion":"dashboard.grafana.app/v1beta1","kind":"Dashboard","metadata":{"name":"sample","namespace":"default"},"spec":{}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			t.Setenv("GRAFANA_SERVER", server.URL)
			path := filepath.Join(t.TempDir(), "config.yaml")
			contents := fmt.Sprintf("version: 1\nstacks:\n  test:\n    grafana:\n      server: %s\n      token: fixture-token\n      org-id: 1\ncontexts:\n  test:\n    stack: test\ncurrent-context: test\n", server.URL)
			require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
			root := &cobra.Command{Use: "gcx", SilenceErrors: true, SilenceUsage: true}
			root.AddCommand(cmdresources.Command())
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			selector := "dashboards"
			if named {
				selector = "dashboards/sample"
			}
			root.SetArgs([]string{"resources", "--config", path, "--context", "test", "delete", selector, "--yes", "--output", "json"})
			require.NoError(t, root.ExecuteContext(t.Context()), stderr.String())
			var result cmdio.BatchMutation
			require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), stdout.String())
			assert.Equal(t, int32(1), reads.Load())
			assert.Zero(t, result.Summary.Failed)
			if named {
				assert.Equal(t, int32(1), deletes.Load())
				assert.Equal(t, 1, result.Summary.Succeeded)
			} else {
				assert.Zero(t, deletes.Load())
				assert.Zero(t, result.Summary.Succeeded)
			}
		})
	}
}
