package resources //nolint:testpackage // runs the real edit command with its configured editor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestEdit_ReadOnlyRefusedBeforeEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("editor fixture uses a POSIX shell")
	}
	for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
		for _, readOnly := range []bool{true, false} {
			name := kind + "/writable"
			if readOnly {
				name = kind + "/read-only"
			}
			t.Run(name, func(t *testing.T) {
				work := t.TempDir()
				marker := filepath.Join(work, "editor-called")
				script := filepath.Join(work, "editor.sh")
				require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nprintf called > \"$GCX_TEST_EDITOR_MARKER\"\n"), 0o700)) //nolint:gosec // The editor control fixture must be executable.
				t.Setenv("EDITOR", script)
				t.Setenv("VISUAL", "")
				t.Setenv("SHELL", "/bin/sh")
				t.Setenv("GCX_TEST_EDITOR_MARKER", marker)
				t.Setenv("GCX_AGENT_MODE", "false")
				t.Setenv("GCX_DISCOVERY_CACHE_DIR", filepath.Join(work, "discovery"))
				group, version := "notifications.alerting.grafana.app", "v1beta1"
				plural, singular := "timeintervals", "timeinterval"
				spec := map[string]any{"name": "weekend"}
				provenance := "converted_prometheus"
				if kind == "TemplateGroup" {
					plural, singular = "templategroups", "templategroup"
					spec = map[string]any{"title": "Built-in Templates", "kind": "grafana"}
					provenance = "system"
				}
				if !readOnly {
					provenance = "api"
				}
				obj := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": group + "/" + version, "kind": kind, "spec": spec,
					"metadata": map[string]any{"name": "opaque", "namespace": "default", "annotations": map[string]any{"grafana.com/provenance": provenance}},
				}}
				var objectReads, writes atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if r.Method != http.MethodGet {
						writes.Add(1)
						http.Error(w, "unexpected write", http.StatusInternalServerError)
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
						gv := metav1.GroupVersionForDiscovery{GroupVersion: group + "/" + version, Version: version}
						result = metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}, Groups: []metav1.APIGroup{{Name: group, Versions: []metav1.GroupVersionForDiscovery{gv}, PreferredVersion: gv}}}
					case "/apis/" + group + "/" + version:
						result = metav1.APIResourceList{GroupVersion: group + "/" + version, APIResources: []metav1.APIResource{{Name: plural, SingularName: singular, Namespaced: true, Kind: kind, Verbs: metav1.Verbs{"get", "list", "create", "update", "delete"}}}}
					case "/apis/" + group + "/" + version + "/namespaces/default/" + plural + "/opaque":
						objectReads.Add(1)
						result = obj
					default:
						t.Errorf("unexpected GET %s", r.URL.Path)
						http.NotFound(w, r)
						return
					}
					assert.NoError(t, json.NewEncoder(w).Encode(result))
				}))
				defer server.Close()
				t.Setenv("GRAFANA_SERVER", server.URL)
				t.Setenv("GRAFANA_STACK_ID", "")
				t.Setenv("GRAFANA_ORG_ID", "1")
				configFile := filepath.Join(work, "config.yaml")
				cfg := fmt.Sprintf("version: 1\ncredentials:\n  keychain: off\nstacks:\n  selected:\n    grafana:\n      server: %s\n      org-id: 1\ncontexts:\n  selected: {stack: selected}\ncurrent-context: selected\n", server.URL)
				require.NoError(t, os.WriteFile(configFile, []byte(cfg), 0o600))
				cmd := editCmd(&cmdconfig.Options{ConfigFile: configFile, Context: "selected"})
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				cmd.SetArgs([]string{plural + "/opaque"})
				err := cmd.ExecuteContext(t.Context())
				if readOnly {
					require.ErrorContains(t, err, "read-only")
					require.ErrorContains(t, err, map[string]string{"system": "built into Grafana", "converted_prometheus": "external Alertmanager configuration"}[provenance])
					require.NoFileExists(t, marker)
				} else {
					require.NoError(t, err)
					require.FileExists(t, marker, "writable control proves the editor stub is callable")
				}
				require.EqualValues(t, 1, objectReads.Load())
				require.Zero(t, writes.Load())
			})
		}
	}
}
