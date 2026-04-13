package config_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTestConfigFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600))
}

func TestResolveTokenPersistenceSource_ExplicitWins(t *testing.T) {
	dir := t.TempDir()
	explicitFile := filepath.Join(dir, "explicit.yaml")
	require.NoError(t, os.WriteFile(explicitFile, []byte("contexts: {}\n"), 0o600))

	got := config.ResolveTokenPersistenceSource(
		t.Context(),
		config.StandardLocation(),
		"default",
		[]config.ConfigSource{{Path: explicitFile, Type: "explicit"}},
	)
	path, err := got()
	require.NoError(t, err)
	assert.Equal(t, explicitFile, path)
}

func TestResolveTokenPersistenceSource_PicksHighestSourceWithOAuthFields(t *testing.T) {
	dir := t.TempDir()
	systemFile := filepath.Join(dir, "system.yaml")
	userFile := filepath.Join(dir, "user.yaml")
	localFile := filepath.Join(dir, "local.yaml")

	require.NoError(t, os.WriteFile(systemFile, []byte(`
contexts:
  default:
    grafana:
      oauth-token: gat_sys
`), 0o600))
	require.NoError(t, os.WriteFile(userFile, []byte(`
contexts:
  default:
    grafana:
      oauth-token: gat_user
`), 0o600))
	require.NoError(t, os.WriteFile(localFile, []byte(`
contexts:
  default:
    grafana:
      oauth-token: gat_local
`), 0o600))

	got := config.ResolveTokenPersistenceSource(
		t.Context(),
		config.StandardLocation(),
		"default",
		[]config.ConfigSource{
			{Path: systemFile, Type: "system"},
			{Path: userFile, Type: "user"},
			{Path: localFile, Type: "local"},
		},
	)
	path, err := got()
	require.NoError(t, err)
	assert.Equal(t, localFile, path)
}

func TestResolveTokenPersistenceSource_FallsBackToUserWhenContextNotFound(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.yaml")
	localFile := filepath.Join(dir, "local.yaml")

	require.NoError(t, os.WriteFile(userFile, []byte("contexts:\n  other: {}\n"), 0o600))
	require.NoError(t, os.WriteFile(localFile, []byte("contexts:\n  other: {}\n"), 0o600))

	got := config.ResolveTokenPersistenceSource(
		t.Context(),
		config.StandardLocation(),
		"default",
		[]config.ConfigSource{
			{Path: userFile, Type: "user"},
			{Path: localFile, Type: "local"},
		},
	)
	path, err := got()
	require.NoError(t, err)
	assert.Equal(t, userFile, path)
}

func TestWireTokenPersistence_ExplicitModeWritesToExplicitSource(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/v1/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_explicit_new",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_explicit_new",
					"refresh_expires_at": "2099-02-01T00:00:00Z",
				},
			})
		case "/bootdata":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{"orgId": 1},
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	explicitFile := filepath.Join(dir, "explicit.yaml")
	userFile := filepath.Join(dir, "user.yaml")
	localFile := filepath.Join(dir, "local.yaml")

	writeTestConfigFile(t, explicitFile, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
current-context: default
`)
	writeTestConfigFile(t, userFile, `
contexts:
  default:
    grafana:
      oauth-token: gat_user
current-context: default
`)
	writeTestConfigFile(t, localFile, `
contexts:
  default:
    grafana:
      oauth-token: gat_local
`)

	restCfg := config.NewNamespacedRESTConfig(t.Context(), config.Context{
		Grafana: &config.GrafanaConfig{
			Server:                srv.URL,
			ProxyEndpoint:         srv.URL,
			OAuthToken:            "gat_old",
			OAuthRefreshToken:     "gar_old",
			OAuthTokenExpiresAt:   "2020-01-01T00:00:00Z",
			OAuthRefreshExpiresAt: "2099-01-01T00:00:00Z",
			StackID:               1,
		},
	})
	restCfg.WireTokenPersistence(
		t.Context(),
		config.ExplicitConfigFile(explicitFile),
		"default",
		[]config.ConfigSource{
			{Path: explicitFile, Type: "explicit"},
			{Path: userFile, Type: "user"},
			{Path: localFile, Type: "local"},
		},
	)

	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	explicitRaw, err := os.ReadFile(explicitFile)
	require.NoError(t, err)
	explicitContents := string(explicitRaw)
	assert.Contains(t, explicitContents, "gat_explicit_new")
	assert.Contains(t, explicitContents, "gar_explicit_new")

	userRaw, err := os.ReadFile(userFile)
	require.NoError(t, err)
	assert.NotContains(t, string(userRaw), "gat_explicit_new")
	assert.NotContains(t, string(userRaw), "gar_explicit_new")

	localRaw, err := os.ReadFile(localFile)
	require.NoError(t, err)
	assert.NotContains(t, string(localRaw), "gat_explicit_new")
	assert.NotContains(t, string(localRaw), "gar_explicit_new")
}

func TestConfig_RESTConfigForContext_WiresTokenPersistence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/v1/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_restcfg_new",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_restcfg_new",
					"refresh_expires_at": "2099-02-01T00:00:00Z",
				},
			})
		case "/bootdata":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"user": map[string]any{"orgId": 1},
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")
	writeTestConfigFile(t, configFile, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_old
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 1
current-context: default
`)

	loaded, err := config.Load(t.Context(), config.ExplicitConfigFile(configFile))
	require.NoError(t, err)
	loaded.Sources = []config.ConfigSource{{Path: configFile, Type: "explicit"}}

	restCfg, err := loaded.RESTConfigForContext(t.Context(), "default", config.ExplicitConfigFile(configFile))
	require.NoError(t, err)

	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	raw, err := os.ReadFile(configFile)
	require.NoError(t, err)
	contents := string(raw)
	assert.Contains(t, contents, "gat_restcfg_new")
	assert.Contains(t, contents, "gar_restcfg_new")
}

func TestConfig_RESTConfigForContext_ContextNotFound(t *testing.T) {
	cfg := config.Config{Contexts: map[string]*config.Context{}}

	_, err := cfg.RESTConfigForContext(t.Context(), "missing", config.StandardLocation())
	require.Error(t, err)
	assert.ErrorContains(t, err, "context not found")
}
