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
	"sync/atomic"
	"testing"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type readOnlyCommandFixture struct {
	config   *cmdconfig.Options
	plural   string
	groupDir string
	readOnly *unstructured.Unstructured
	reads    atomic.Int32
	writes   atomic.Int32
}

func newReadOnlyCommandFixture(t *testing.T, kind string) *readOnlyCommandFixture {
	t.Helper()
	// Register before Setenv so detection is restored after the environment.
	t.Cleanup(agent.ResetForTesting)
	work := t.TempDir()
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", filepath.Join(work, "discovery"))
	const group = "notifications.alerting.grafana.app"
	const version = "v1beta1"
	f := &readOnlyCommandFixture{plural: "timeintervals"}
	singular := "timeinterval"
	if kind == "TemplateGroup" {
		f.plural, singular = "templategroups", "templategroup"
	}
	f.groupDir = f.plural + "." + version + "." + group
	object := func(name string, readOnly bool) *unstructured.Unstructured {
		provenance := "api"
		spec := map[string]any{"name": name, "time_intervals": []any{}}
		if kind == "TemplateGroup" {
			spec = map[string]any{"title": name, "kind": "grafana", "content": "{{ define \"test\" }}hello{{ end }}"}
		}
		if readOnly {
			provenance = "converted_prometheus"
			if kind == "TemplateGroup" {
				provenance = "system"
			}
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": group + "/" + version, "kind": kind, "spec": spec,
			"metadata": map[string]any{"name": name, "namespace": "default", "annotations": map[string]any{"grafana.com/provenance": provenance}},
		}}
	}
	f.readOnly = object("read-only", true)
	writable := object("writable", false)
	base := "/apis/" + group + "/" + version + "/namespaces/default/" + f.plural
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			f.writes.Add(1)
			t.Errorf("unexpected mutation %s %s", r.Method, r.URL.Path)
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
			gv := metav1.GroupVersionForDiscovery{GroupVersion: group + "/" + version, Version: version}
			result = metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList", APIVersion: "v1"}, Groups: []metav1.APIGroup{{Name: group, Versions: []metav1.GroupVersionForDiscovery{gv}, PreferredVersion: gv}}}
		case "/apis/" + group + "/" + version:
			result = metav1.APIResourceList{GroupVersion: group + "/" + version, APIResources: []metav1.APIResource{{Name: f.plural, SingularName: singular, Namespaced: true, Kind: kind, Verbs: metav1.Verbs{"get", "list", "create", "update", "delete"}}}}
		case base:
			f.reads.Add(1)
			result = &unstructured.UnstructuredList{Object: map[string]any{"apiVersion": group + "/" + version, "kind": kind + "List"}, Items: []unstructured.Unstructured{*f.readOnly, *writable}}
		case base + "/read-only":
			f.reads.Add(1)
			result = f.readOnly
		default:
			t.Errorf("unexpected GET %s", r.URL.Path)
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

func TestPull_ReadOnlyExclusionWiring(t *testing.T) {
	for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
		for _, mode := range []string{"human", "agent"} {
			for _, skip := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/skip=%t", kind, mode, skip), func(t *testing.T) {
					f := newReadOnlyCommandFixture(t, kind)
					if mode == "agent" {
						t.Setenv("GCX_AGENT_MODE", "true")
					}
					agent.ResetForTesting()
					destination := filepath.Join(t.TempDir(), "resources")
					var stdout bytes.Buffer
					cmd := pullCmd(f.config)
					cmd.SetOut(&stdout)
					cmd.SetErr(io.Discard)
					args := []string{f.plural, "--path", destination}
					if skip {
						args = append(args, "--skip-read-only")
					}
					cmd.SetArgs(args)
					require.NoError(t, cmd.ExecuteContext(t.Context()))
					require.FileExists(t, filepath.Join(destination, f.groupDir, "writable.json"))
					expected := cmdio.MutationSummary{Succeeded: 2}
					var reasons map[string]int
					if skip {
						require.NoFileExists(t, filepath.Join(destination, f.groupDir, "read-only.json"))
						expected = cmdio.MutationSummary{Succeeded: 1, Skipped: 1}
						reasons = map[string]int{"read-only": 1}
					} else {
						require.FileExists(t, filepath.Join(destination, f.groupDir, "read-only.json"))
					}
					switch {
					case mode == "agent":
						var receipt cmdio.ArtifactReceipt
						require.NoError(t, json.Unmarshal(stdout.Bytes(), &receipt))
						require.Equal(t, reasons, receipt.SkippedReasons)
						require.Equal(t, expected, receipt.Summary)
					case skip:
						require.Contains(t, stdout.String(), "1 resources pulled, 0 errors (1 skipped: read-only)")
					default:
						require.Contains(t, stdout.String(), "2 resources pulled, 0 errors")
						require.NotContains(t, stdout.String(), "skipped")
					}
					require.EqualValues(t, 1, f.reads.Load())
					require.Zero(t, f.writes.Load())
				})
			}
		}
	}
}

func TestMutation_ReadOnlySkipReasonWiring(t *testing.T) {
	for _, kind := range []string{"TimeInterval", "TemplateGroup"} {
		for _, operation := range []string{"push", "delete-files", "delete-selector"} {
			for _, output := range []string{"text", "json", "agent-default"} {
				t.Run(kind+"/"+operation+"/"+output, func(t *testing.T) {
					f := newReadOnlyCommandFixture(t, kind)
					if output == "agent-default" {
						t.Setenv("GCX_AGENT_MODE", "true")
					}
					agent.ResetForTesting()
					path := filepath.Join(t.TempDir(), "read-only.json")
					data, err := json.Marshal(f.readOnly)
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(path, data, 0o600))
					cmd := deleteCmd(f.config)
					args := []string{"--path", path, "--force"}
					action := "deleted"
					switch operation {
					case "push":
						cmd = pushCmd(f.config)
						args = []string{"--path", path}
						action = "pushed"
					case "delete-selector":
						args = []string{f.plural + "/read-only"}
					}
					if output != "agent-default" {
						args = append(args, "--output", output)
					}
					var stdout bytes.Buffer
					cmd.SetOut(&stdout)
					cmd.SetErr(io.Discard)
					cmd.SetArgs(args)
					require.NoError(t, cmd.ExecuteContext(t.Context()))
					if output == "text" {
						require.Contains(t, stdout.String(), "0 resources "+action+", 0 errors (1 skipped: read-only)")
						require.NotContains(t, stdout.String(), "not server-verified", "the command must pass the actual skip reason to its text codec")
					} else {
						var result cmdio.BatchMutation
						require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
						require.Equal(t, map[string]int{"read-only": 1}, result.SkippedReasons)
						require.Equal(t, cmdio.MutationSummary{Skipped: 1}, result.Summary)
					}
					require.Zero(t, f.writes.Load())
					if operation == "delete-selector" {
						require.EqualValues(t, 1, f.reads.Load())
					}
				})
			}
		}
	}
}
