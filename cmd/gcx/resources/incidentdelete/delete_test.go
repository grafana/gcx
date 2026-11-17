package incidentdelete_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	cmdresources "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	_ "github.com/grafana/gcx/internal/providers/irm" // Register the incident adapter.
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeleteMixedSelectorContinuesAfterUnsupportedIncident(t *testing.T) {
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
	t.Setenv("GCX_KEYCHAIN", "off")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	for _, name := range []string{"GRAFANA_TOKEN", "GRAFANA_USER", "GRAFANA_PASSWORD", "GRAFANA_PROXY_ENDPOINT"} {
		t.Setenv(name, "")
	}

	const groupVersion = "dashboard.grafana.app/v1beta1"
	const resourcePath = "/apis/" + groupVersion + "/namespaces/default/dashboards"
	var dashboardDeletes, incidentWrites atomic.Int32
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
			_, _ = w.Write([]byte(`{"apiVersion":"dashboard.grafana.app/v1beta1","kind":"DashboardList","items":[{"apiVersion":"dashboard.grafana.app/v1beta1","kind":"Dashboard","metadata":{"name":"sample","namespace":"default"},"spec":{}}]}`))
		case resourcePath + "/sample":
			if r.Method == http.MethodDelete {
				dashboardDeletes.Add(1)
				_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Status","status":"Success"}`))
				return
			}
			http.NotFound(w, r)
		default:
			if strings.Contains(r.URL.Path, "IncidentsService.QueryIncidentPreviews") {
				if r.Method != http.MethodPost {
					incidentWrites.Add(1)
				}
				_, _ = w.Write([]byte(`{"incidentPreviews":[{"incidentID":"inc-1","title":"Outage","status":"active"}],"cursor":{"hasMore":false}}`))
				return
			}
			if strings.Contains(r.URL.Path, "IncidentsService") {
				incidentWrites.Add(1)
			}
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
	root.SetArgs([]string{"resources", "--config", path, "--context", "test", "delete", "dashboards", "incidents", "--force", "--output", "json"})
	err := root.ExecuteContext(t.Context())
	require.Error(t, err)
	var emitted *gcxerrors.EmittedError
	require.ErrorAs(t, err, &emitted)
	assert.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
	var result cmdio.BatchMutation
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result), stdout.String())
	assert.Equal(t, 1, result.Summary.Succeeded)
	assert.Equal(t, 1, result.Summary.Failed)
	assert.Equal(t, int32(1), dashboardDeletes.Load())
	assert.Zero(t, incidentWrites.Load())
}
