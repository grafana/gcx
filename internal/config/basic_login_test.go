package config_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasicLoginKeychain(t *testing.T) {
	for _, method := range []string{"token", "oauth", "basic"} {
		t.Run(method, func(t *testing.T) {
			store := withFakeStore(t)
			t.Setenv("GCX_KEYCHAIN", "on")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				user, password, ok := r.BasicAuth()
				if r.URL.Path != "/api/user" || !ok || user != "new-user" || password != "new-password" {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
					return
				}
				_, _ = w.Write([]byte(`{"id":1,"login":"new-user"}`))
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "config.yaml")
			initial := fmt.Sprintf(`version: 1
stacks:
  local:
    grafana:
      server: %s
      auth-method: %s
      org-id: 1
      user: old-user
      password: old-password
      token: old-token
      oauth-token: old-oauth
      oauth-refresh-token: old-refresh
contexts:
  local: {stack: local}
current-context: local
`, server.URL, method)
			source := config.ExplicitConfigFile(path)
			require.NoError(t, os.WriteFile(path, []byte(initial), 0o600))
			cfg, err := config.Load(t.Context(), source)
			require.NoError(t, err)
			require.NoError(t, config.Write(t.Context(), source, cfg))
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			count := store.len()
			opts := login.Options{
				Inputs: login.Inputs{Server: server.URL, ContextName: "local", Target: login.TargetOnPrem, UseBasicAuth: true, GrafanaUser: "new-user", GrafanaPassword: "wrong-password", Yes: true},
				Hooks: login.Hooks{ConfigSource: source, ValidateFn: func(context.Context, login.Options, config.NamespacedRESTConfig) (string, error) {
					return "12.0.0", nil
				}},
			}
			// Even save-anyway must verify the credentials before touching either store.
			opts.ForceSave = true
			_, err = login.Run(t.Context(), &opts)
			var authErr *login.BasicAuthCheckError
			require.ErrorAs(t, err, &authErr)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			assert.Equal(t, count, store.len())
			assert.True(t, store.containsValue("old-password"))
			assert.False(t, store.containsValue("wrong-password"))

			opts.ForceSave = false
			opts.GrafanaPassword = "new-password"
			result, err := login.Run(t.Context(), &opts)
			require.NoError(t, err)
			assert.Equal(t, "basic", result.AuthMethod)
			after, err = os.ReadFile(path)
			require.NoError(t, err)
			assert.Contains(t, string(after), "keychain:gcx:v2:")
			assert.NotContains(t, string(after), "new-password")
			assert.True(t, store.containsValue("new-password"))
			loaded, err := config.Load(t.Context(), source)
			require.NoError(t, err)
			grafana := loaded.Contexts["local"].Grafana
			assert.Equal(t, "basic", grafana.AuthMethod)
			assert.Equal(t, "new-user", grafana.User)
			assert.Equal(t, "new-password", grafana.Password)
			assert.Empty(t, grafana.APIToken)
			assert.Empty(t, grafana.OAuthToken)
			assert.Empty(t, grafana.OAuthRefreshToken)
		})
	}
}
