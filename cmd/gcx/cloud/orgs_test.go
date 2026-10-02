//nolint:testpackage // exercises the internal orgs command constructor and real config loader.
package cloud

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgsListCommand(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		body   string
		status int
		agent  bool
		want   string
		fail   bool
	}{
		{"table", nil, `[{"login":"example-org","role":"Admin"}]`, 200, false, "example-org", false},
		{"json", []string{"-o", "json"}, `[]`, 200, false, "[]", false},
		{"agent", nil, `[{"login":"example-org","role":"Admin"}]`, 200, true, `"slug"`, false},
		{"yaml", []string{"-o", "yaml"}, `[{"login":"example-org","role":"Admin"}]`, 200, false, "slug: example-org", false},
		{"projection", []string{"--json", "slug"}, `[{"login":"example-org","role":"Admin"}]`, 200, false, `"slug"`, false},
		{"scope denied", nil, `{}`, 403, false, "profile", true},
		{"not a user", nil, `null`, 200, false, "user OAuth", true},
		{"extra arg", []string{"extra"}, `[]`, 200, false, "", true},
		{"bad format", []string{"-o", "bogus"}, `[]`, 200, false, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			agent.SetFlag(tc.agent)
			t.Cleanup(agent.ResetForTesting)
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, "/api/oauth2/user/orgs", r.URL.Path)
				assert.Equal(t, "Bearer example-token", r.Header.Get("Authorization"))
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("version: 1\ncontexts:\n  selected:\n    cloud: selected-cloud\ncloud:\n  selected-cloud:\n    token: example-token\n    api-url: "+srv.URL+"\ncurrent-context: selected\n"), 0600))
			cmd := orgsCommand()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			cmd.SetArgs(append([]string{"list", "--config", path}, tc.args...))
			err := cmd.Execute()
			if tc.fail {
				require.Error(t, err)
				if tc.name == "scope denied" {
					var detailed *gcxerrors.DetailedError
					require.ErrorAs(t, err, &detailed)
					require.Contains(t, detailed.Suggestions[0], tc.want)
				} else if tc.want != "" {
					require.Contains(t, err.Error(), tc.want)
				}
			} else {
				require.NoError(t, err)
				require.Contains(t, out.String(), tc.want)
			}
			if tc.name == "extra arg" || tc.name == "bad format" {
				require.Zero(t, requests)
			} else {
				require.Equal(t, 1, requests)
			}
		})
	}
}
