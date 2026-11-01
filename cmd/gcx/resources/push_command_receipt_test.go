package resources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	resourcescmd "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func TestPushCommandReturnedIdentity(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			_, _ = fmt.Fprint(w, `{"kind":"APIVersions","versions":[]}`)
		case "/apis":
			_, _ = fmt.Fprint(w, `{"kind":"APIGroupList","groups":[{"name":"receipt.test.grafana.app","versions":[{"groupVersion":"receipt.test.grafana.app/v1","version":"v1"}],"preferredVersion":{"groupVersion":"receipt.test.grafana.app/v1","version":"v1"}}]}`)
		case "/apis/receipt.test.grafana.app/v1":
			_, _ = fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"receipt.test.grafana.app/v1","resources":[{"name":"items","singularName":"item","namespaced":true,"kind":"Item","verbs":["get","list","create","update"]}]}`)
		case "/apis/receipt.test.grafana.app/v1/namespaces/default/items":
			if r.Method == http.MethodPost {
				_, _ = fmt.Fprint(w, `{"apiVersion":"receipt.test.grafana.app/v1","kind":"Item","metadata":{"name":"server-name","uid":"server-uid","namespace":"default"}}`)
			} else {
				_, _ = fmt.Fprint(w, `{"kind":"ItemList","apiVersion":"receipt.test.grafana.app/v1","items":[]}`)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		}
	}))
	t.Cleanup(server.Close)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfg, fmt.Appendf(nil, "version: 1\nstacks:\n  local:\n    grafana:\n      server: %s\n      org-id: 1\ncontexts:\n  local:\n    stack: local\ncurrent-context: local\n", server.URL), 0o600))
	path := filepath.Join(t.TempDir(), "item.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"apiVersion":"receipt.test.grafana.app/v1","kind":"Item","metadata":{}}`), 0o600))
	for _, outputFormat := range []string{"json", "yaml"} {
		for _, tc := range []struct{ include, dryRun bool }{{false, false}, {true, false}, {true, true}} {
			include := tc.include
			root := &cobra.Command{Use: "gcx"}
			root.AddCommand(resourcescmd.Command())
			var stdout bytes.Buffer
			root.SetOut(&stdout)
			var stderr bytes.Buffer
			root.SetErr(&stderr)
			args := []string{"resources", "--config", cfg, "push", "--path", path, "--output", outputFormat, "--omit-manager-fields"}
			if include {
				args = append(args, "--include-successes")
			}
			if tc.dryRun {
				args = append(args, "--dry-run")
			}
			root.SetArgs(args)
			err := root.Execute()
			require.NoError(t, err, stdout.String())
			var result struct {
				Successes []struct {
					Requested struct {
						SourcePath string `json:"source_path"`
					} `json:"requested"`
					Target struct {
						Name string `json:"name"`
					} `json:"target"`
				} `json:"successes"`
			}
			jsonData := stdout.Bytes()
			if outputFormat == "yaml" {
				var err error
				jsonData, err = yaml.YAMLToJSON(jsonData)
				require.NoError(t, err)
			}
			require.NoError(t, json.Unmarshal(jsonData, &result))
			if !include {
				var document map[string]any
				require.NoError(t, json.Unmarshal(jsonData, &document))
				require.NotContains(t, document, "successes")
			}
			if include && !tc.dryRun {
				require.Len(t, result.Successes, 1)
				require.Equal(t, path, result.Successes[0].Requested.SourcePath)
				require.Equal(t, "server-name", result.Successes[0].Target.Name)
			} else {
				require.Empty(t, result.Successes)
				if tc.dryRun {
					require.NotContains(t, stderr.String(), "no real write")
					var document map[string]any
					require.NoError(t, json.Unmarshal(jsonData, &document))
					require.Equal(t, []any{}, document["successes"])
				}
			}
		}
	}
}

func TestPushCommandAbortPreservesReturnedIdentity(t *testing.T) {
	t.Setenv("GCX_AGENT_MODE", "false")
	t.Setenv("GCX_DISCOVERY_CACHE_DIR", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api":
			_, _ = fmt.Fprint(w, `{"kind":"APIVersions","versions":[]}`)
		case "/apis":
			_, _ = fmt.Fprint(w, `{"kind":"APIGroupList","groups":[{"name":"folder.grafana.app","versions":[{"groupVersion":"folder.grafana.app/v1","version":"v1"}],"preferredVersion":{"groupVersion":"folder.grafana.app/v1","version":"v1"}},{"name":"receipt.test.grafana.app","versions":[{"groupVersion":"receipt.test.grafana.app/v1","version":"v1"}],"preferredVersion":{"groupVersion":"receipt.test.grafana.app/v1","version":"v1"}}]}`)
		case "/apis/folder.grafana.app/v1":
			_, _ = fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"folder.grafana.app/v1","resources":[{"name":"folders","singularName":"folder","namespaced":true,"kind":"Folder","verbs":["get","list","create","update"]}]}`)
		case "/apis/receipt.test.grafana.app/v1":
			_, _ = fmt.Fprint(w, `{"kind":"APIResourceList","groupVersion":"receipt.test.grafana.app/v1","resources":[{"name":"items","singularName":"item","namespaced":true,"kind":"Item","verbs":["get","list","create","update"]}]}`)
		case "/apis/folder.grafana.app/v1/namespaces/default/folders":
			_, _ = fmt.Fprint(w, `{"apiVersion":"folder.grafana.app/v1","kind":"Folder","metadata":{"name":"server-folder","uid":"folder-uid","namespace":"default"}}`)
		case "/apis/receipt.test.grafana.app/v1/namespaces/default/items":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"InternalError","message":"write denied","code":500}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		}
	}))
	t.Cleanup(server.Close)
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(cfg, fmt.Appendf(nil, "version: 1\nstacks:\n  local:\n    grafana:\n      server: %s\n      org-id: 1\ncontexts:\n  local:\n    stack: local\ncurrent-context: local\n", server.URL), 0o600))
	path := t.TempDir()
	folderPath := filepath.Join(path, "folder.json")
	require.NoError(t, os.WriteFile(folderPath, []byte(`{"apiVersion":"folder.grafana.app/v1","kind":"Folder","metadata":{"name":"input-folder"},"spec":{"title":"Folder"}}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(path, "item.json"), []byte(`{"apiVersion":"receipt.test.grafana.app/v1","kind":"Item","metadata":{"name":"failed-item"}}`), 0o600))

	for _, tc := range []struct {
		include bool
		format  string
	}{{false, "json"}, {true, "json"}, {true, "text"}} {
		t.Run(fmt.Sprintf("include=%v format=%s", tc.include, tc.format), func(t *testing.T) {
			root := &cobra.Command{Use: "gcx", SilenceUsage: true, SilenceErrors: true}
			root.AddCommand(resourcescmd.Command())
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			args := []string{"resources", "--config", cfg, "push", "--path", path, "--output", tc.format, "--on-error", "abort", "--max-concurrent", "1", "--omit-manager-fields"}
			if tc.include {
				args = append(args, "--include-successes")
			}
			root.SetArgs(args)
			err := root.Execute()
			require.Error(t, err)
			if !tc.include {
				require.Empty(t, stdout.String())
				return
			}
			var emitted *gcxerrors.EmittedError
			require.ErrorAs(t, err, &emitted)
			require.Equal(t, gcxerrors.ExitPartialFailure, emitted.Code)
			require.ErrorContains(t, emitted, "write denied")
			if tc.format == "text" {
				require.Contains(t, stderr.String(), "write denied")
				require.Contains(t, stdout.String(), "1 resources pushed, 1 errors")
				return
			}
			var result struct {
				Summary struct {
					Succeeded int `json:"succeeded"`
					Failed    int `json:"failed"`
				} `json:"summary"`
				Successes []struct {
					Requested struct {
						SourcePath string `json:"source_path"`
					} `json:"requested"`
					Target struct {
						Name string `json:"name"`
					} `json:"target"`
				} `json:"successes"`
				Failures []any `json:"failures"`
			}
			decoder := json.NewDecoder(&stdout)
			require.NoError(t, decoder.Decode(&result))
			require.ErrorIs(t, decoder.Decode(&struct{}{}), io.EOF)
			require.Equal(t, 1, result.Summary.Succeeded)
			require.Equal(t, 1, result.Summary.Failed)
			require.Len(t, result.Successes, 1)
			require.Equal(t, folderPath, result.Successes[0].Requested.SourcePath)
			require.Equal(t, "server-folder", result.Successes[0].Target.Name)
			require.Len(t, result.Failures, 1)
		})
	}
}
