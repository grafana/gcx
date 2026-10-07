package root_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudOrgsAuthErrorProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the gcx binary")
	}
	for _, tc := range []struct {
		name          string
		status        int
		body          string
		details       string
		summary       string
		serverMessage string
	}{
		{"unauthorized", 401, `{"message":"token expired"}`, "failed to list cloud organisations: token expired (HTTP 401)", "Authentication failed", "token expired"},
		{"forbidden", 403, `{"message":"profile scope missing"}`, "failed to list cloud organisations: profile scope missing (HTTP 403)", "Authorization failed", "profile scope missing"},
		{"no user", 200, `null`, "Organisation listing requires a browser Cloud OAuth login.", "Authentication failed", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/oauth2/user/orgs", r.URL.Path)
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			config := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(config, []byte("version: 1\ncontexts:\n  test:\n    cloud: test\ncloud:\n  test:\n    token: fake-test-token\n    api-url: "+server.URL+"\ncurrent-context: test\n"), 0600))
			stdout, code := runGcx(t, "cloud", "orgs", "list", "--config", config)
			require.Equal(t, 3, code)
			document, ok := assertOneJSONValue(t, stdout).(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "gcx.error", document["type"])
			failure, ok := document["error"].(map[string]any)
			require.True(t, ok)
			assert.EqualValues(t, 3, failure["exitCode"])
			assert.Equal(t, tc.summary, failure["summary"])
			details, ok := failure["details"].(string)
			require.True(t, ok)
			assert.Equal(t, tc.details, details)
			if tc.serverMessage != "" {
				assert.Equal(t, 1, strings.Count(details, tc.serverMessage))
			}
			suggestions, ok := failure["suggestions"].([]any)
			require.True(t, ok)
			require.Len(t, suggestions, 2)
			if tc.status == http.StatusForbidden {
				assert.Contains(t, suggestions[0], "scopes")
				assert.Contains(t, suggestions[0], "profile")
				assert.Contains(t, suggestions[0], "permissions")
				assert.Contains(t, suggestions[1], "gcx setup status")
				for _, suggestion := range suggestions {
					assert.NotContains(t, suggestion, "gcx cloud login")
				}
			} else {
				assert.Contains(t, suggestions[0], "gcx cloud login")
				assert.NotContains(t, suggestions[0], "--scope profile")
				assert.Contains(t, suggestions[1], "GRAFANA_CLOUD_TOKEN")
				assert.Contains(t, suggestions[1], "cloud.<entry>.token")
			}
		})
	}
}
