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
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrgsListCommand(t *testing.T) {
	for _, tc := range []struct {
		name         string
		args         []string
		body         string
		status       int
		agent        bool
		want         string
		fail         bool
		wantDetailed bool
		wantRequests int
		capToken     string
		envToken     string
	}{
		{name: "table", body: `[{"login":"example-org","role":"Admin"}]`, status: 200, want: "example-org", wantRequests: 1},
		{name: "json", args: []string{"-o", "json"}, body: `[]`, status: 200, want: "[]", wantRequests: 1},
		{name: "agent", body: `[{"login":"example-org","role":"Admin"}]`, status: 200, agent: true, want: `"slug"`, wantRequests: 1},
		{name: "yaml", args: []string{"-o", "yaml"}, body: `[{"login":"example-org","role":"Admin"}]`, status: 200, want: "slug: example-org", wantRequests: 1},
		{name: "projection", args: []string{"--json", "slug"}, body: `[{"login":"example-org","role":"Admin"}]`, status: 200, want: `"slug"`, wantRequests: 1},
		{name: "scope denied", body: `{}`, status: 403, want: "profile", fail: true, wantDetailed: true, wantRequests: 1},
		{name: "configured CAP shadows OAuth", capToken: "example-cap", body: `{}`, status: 403, want: "profile", fail: true, wantDetailed: true, wantRequests: 1},
		{name: "environment CAP shadows OAuth", envToken: "example-env-cap", body: `{}`, status: 403, want: "profile", fail: true, wantDetailed: true, wantRequests: 1},
		{name: "not a user", body: `null`, status: 200, want: "GRAFANA_CLOUD_TOKEN", fail: true, wantRequests: 1},
		{name: "extra arg", args: []string{"extra"}, fail: true},
		{name: "bad format", args: []string{"-o", "bogus"}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutils.SandboxConfigEnv(t)
			agent.SetFlag(tc.agent)
			t.Cleanup(agent.ResetForTesting)
			requests := 0
			token := "example-token"
			if tc.capToken != "" {
				token = tc.capToken
			}
			if tc.envToken != "" {
				t.Setenv("GRAFANA_CLOUD_TOKEN", tc.envToken)
				token = tc.envToken
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				assert.Equal(t, "/api/oauth2/user/orgs", r.URL.Path)
				assert.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte("version: 1\ncontexts:\n  selected:\n    cloud: selected-cloud\ncloud:\n  selected-cloud:\n    oauth-token: example-token\n    oauth-token-expires-at: "+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+"\n    token: "+tc.capToken+"\n    oauth-url: "+srv.URL+"\n    api-url: "+srv.URL+"\ncurrent-context: selected\n"), 0600))
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
				if tc.wantDetailed {
					var detailed *gcxerrors.DetailedError
					require.ErrorAs(t, err, &detailed)
					require.Contains(t, detailed.Suggestions[0], tc.want)
					require.Contains(t, detailed.Suggestions[0], "gcx cloud login")
					require.NotContains(t, detailed.Suggestions[0], "--scope profile")
					require.Contains(t, detailed.Suggestions[1], "GRAFANA_CLOUD_TOKEN")
					require.Contains(t, detailed.Suggestions[1], "cloud.<entry>.token")
				} else if tc.want != "" {
					require.Contains(t, err.Error(), tc.want)
				}
			} else {
				require.NoError(t, err)
				require.Contains(t, out.String(), tc.want)
			}
			require.Equal(t, tc.wantRequests, requests)
		})
	}
}
