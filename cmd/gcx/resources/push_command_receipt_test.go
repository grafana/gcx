package resources_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	resourcescmd "github.com/grafana/gcx/cmd/gcx/resources"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.WriteFile(path, []byte(`{"apiVersion":"receipt.test.grafana.app/v1","kind":"Item","metadata":{"name":"requested-name"}}`), 0o600))
	for _, include := range []bool{false, true} {
		root := &cobra.Command{Use: "gcx"}
		root.AddCommand(resourcescmd.Command())
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetErr(&bytes.Buffer{})
		args := []string{"resources", "--config", cfg, "push", "--path", path, "--output", "json", "--omit-manager-fields"}
		if include {
			args = append(args, "--include-successes")
		}
		root.SetArgs(args)
		err := root.Execute()
		require.NoError(t, err, stdout.String())
		var result struct {
			Successes []struct {
				Target struct {
					Name string `json:"name"`
				} `json:"target"`
			} `json:"successes"`
		}
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
		if include {
			require.Len(t, result.Successes, 1)
			require.Equal(t, "server-name", result.Successes[0].Target.Name)
		} else {
			require.Empty(t, result.Successes)
		}
	}
}
