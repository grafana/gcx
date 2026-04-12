package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/grafana/gcx/internal/cloud"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newMockGCOMServer returns an httptest.Server that responds to any request
// with the given StackInfo encoded as JSON.
func newMockGCOMServer(t *testing.T, info cloud.StackInfo) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(info); err != nil {
			t.Errorf("mock GCOM server: encode response: %v", err)
		}
	}))
}

// writeConfigFile writes YAML content to a temp file and returns its path.
func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "gcx-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600))
}

func TestConfigLoader_LoadCloudConfig_MissingToken(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	_, err := loader.LoadCloudConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cloud token is required")
}

func TestConfigLoader_LoadCloudConfig_MissingStack(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    cloud:
      token: "my-token"
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	_, err := loader.LoadCloudConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cloud stack is not configured")
}

// TestConfigLoader_LoadCloudConfig_EnvVars verifies that GRAFANA_CLOUD_TOKEN and
// GRAFANA_CLOUD_STACK env vars are picked up even when the config file has no
// cloud section.
func TestConfigLoader_LoadCloudConfig_EnvVars(t *testing.T) {
	// Config file has api-url pointing at our test server (the scheme is supplied
	// by ResolveGCOMURL as "https://", so we can't use the test server's plain
	// HTTP URL here — but we still verify that env vars are parsed and validation
	// passes by checking the error is a network error, not a validation error).
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)

	t.Setenv("GRAFANA_CLOUD_TOKEN", "env-token")
	t.Setenv("GRAFANA_CLOUD_STACK", "mystack")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	_, err := loader.LoadCloudConfig(context.Background())
	// The GCOM call will fail (no real GCOM server), but it must NOT fail with a
	// validation error about missing token or stack — those were set via env vars.
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "cloud token is required")
	assert.NotContains(t, err.Error(), "cloud stack is not configured")
}

// TestConfigLoader_LoadCloudConfig_GCOMCallAttempted verifies that when token and
// stack are configured, LoadCloudConfig actually attempts to call the GCOM API
// (the error is a network error, not a validation error).
func TestConfigLoader_LoadCloudConfig_GCOMCallAttempted(t *testing.T) {
	srv := newMockGCOMServer(t, cloud.StackInfo{ID: 42, Slug: "mystack"})
	defer srv.Close()

	// ResolveGCOMURL prepends "https://"; our test server is HTTP only. We
	// write api-url without the scheme so ResolveGCOMURL adds "https://".
	// This means the connection will fail at TLS, proving the GCOM call
	// was attempted (rather than a validation failure).
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    cloud:
      token: "test-token"
      stack: "mystack"
      api-url: "`+srv.URL[len("http://"):]+`"
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	_, err := loader.LoadCloudConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get stack info")
	assert.NotContains(t, err.Error(), "cloud token is required")
	assert.NotContains(t, err.Error(), "cloud stack is not configured")
}

// TestConfigLoader_LoadProviderConfig tests LoadProviderConfig with env vars and config file.
func TestConfigLoader_LoadProviderConfig(t *testing.T) {
	tests := []struct {
		name         string
		configYAML   string
		envVars      map[string]string
		providerName string
		wantConfig   map[string]string
		wantErr      bool
	}{
		{
			// AC-1: env var overrides everything
			name: "env_var_only",
			configYAML: `
contexts:
  default: {}
current-context: default
`,
			envVars:      map[string]string{"GRAFANA_PROVIDER_SYNTH_SM_URL": "https://env.sm"},
			providerName: "synth",
			wantConfig:   map[string]string{"sm-url": "https://env.sm"},
		},
		{
			// AC-2: config file value returned when no env var
			name: "config_file_only",
			configYAML: `
contexts:
  default:
    providers:
      synth:
        sm-url: https://file.sm
current-context: default
`,
			providerName: "synth",
			wantConfig:   map[string]string{"sm-url": "https://file.sm"},
		},
		{
			// AC-3: env var takes precedence over config file
			name: "env_var_overrides_config_file",
			configYAML: `
contexts:
  default:
    providers:
      synth:
        sm-url: https://file.sm
current-context: default
`,
			envVars:      map[string]string{"GRAFANA_PROVIDER_SYNTH_SM_URL": "https://env.sm"},
			providerName: "synth",
			wantConfig:   map[string]string{"sm-url": "https://env.sm"},
		},
		{
			// provider not in config → nil map returned (no error)
			name: "provider_not_configured",
			configYAML: `
contexts:
  default: {}
current-context: default
`,
			providerName: "synth",
			wantConfig:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfgFile := writeConfigFile(t, tc.configYAML)
			for k, v := range tc.envVars {
				t.Setenv(k, v)
			}

			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(cfgFile)

			got, _, err := loader.LoadProviderConfig(context.Background(), tc.providerName)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantConfig, got)
		})
	}
}

// TestConfigLoader_LoadProviderConfig_Namespace verifies that namespace is returned.
func TestConfigLoader_LoadProviderConfig_Namespace(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	_, namespace, err := loader.LoadProviderConfig(context.Background(), "synth")
	require.NoError(t, err)
	assert.Equal(t, "default", namespace)
}

func TestConfigLoader_SaveDatasourceUID(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	err := loader.SaveDatasourceUID(context.Background(), "tempo", "tempo-123")
	require.NoError(t, err)

	loaded, err := loader.LoadFullConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, loaded.GetCurrentContext())
	assert.Equal(t, "tempo-123", loaded.GetCurrentContext().Datasources["tempo"])
}

func TestConfigLoader_SaveDatasourceUID_PreservesCurrentContext(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
  other: {}
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	loader.SetContextName("other")

	err := loader.SaveDatasourceUID(context.Background(), "tempo", "tempo-123")
	require.NoError(t, err)

	raw, err := internalconfig.Load(context.Background(), internalconfig.ExplicitConfigFile(cfgFile))
	require.NoError(t, err)
	assert.Equal(t, "default", raw.CurrentContext)
	require.NotNil(t, raw.Contexts["other"])
	assert.Equal(t, "tempo-123", raw.Contexts["other"].Datasources["tempo"])
}

func TestConfigLoader_SaveDatasourceUID_DoesNotPersistEnvOverrides(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)
	t.Setenv("GRAFANA_SERVER", "https://env.example.com")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	err := loader.SaveDatasourceUID(context.Background(), "tempo", "tempo-123")
	require.NoError(t, err)

	raw, err := internalconfig.Load(context.Background(), internalconfig.ExplicitConfigFile(cfgFile))
	require.NoError(t, err)
	require.NotNil(t, raw.GetCurrentContext())
	assert.Equal(t, "tempo-123", raw.GetCurrentContext().Datasources["tempo"])
	if raw.GetCurrentContext().Grafana != nil {
		assert.Empty(t, raw.GetCurrentContext().Grafana.Server)
	}
}

func TestConfigLoader_SaveDatasourceUID_SkipsWhenNoConfigExists(t *testing.T) {
	homeDir := t.TempDir()
	xdgDir := t.TempDir()
	workDir := t.TempDir()

	t.Chdir(workDir)

	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdg.Reload()

	loader := &providers.ConfigLoader{}
	err := loader.SaveDatasourceUID(context.Background(), "tempo", "tempo-123")
	require.NoError(t, err)

	// Verify no config file was created on disk.
	standardPath := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	_, err = os.Stat(standardPath)
	assert.True(t, os.IsNotExist(err), "config file should not have been created at %s", standardPath)

	xdgPath := filepath.Join(xdgDir, "gcx", "config.yaml")
	_, err = os.Stat(xdgPath)
	assert.True(t, os.IsNotExist(err), "config file should not have been created at %s", xdgPath)
}

func TestConfigLoader_SaveDatasourceUID_ErrorsWithMultipleConfigSources(t *testing.T) {
	homeDir := t.TempDir()
	xdgDir := t.TempDir()
	workDir := t.TempDir()

	userFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o755))
	require.NoError(t, os.WriteFile(userFile, []byte("contexts:\n  default: {}\ncurrent-context: default\n"), 0o600))

	localFile := filepath.Join(workDir, ".gcx.yaml")
	require.NoError(t, os.WriteFile(localFile, []byte("contexts:\n  default: {}\ncurrent-context: default\n"), 0o600))

	t.Chdir(workDir)

	t.Cleanup(func() { xdg.Reload() })
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdg.Reload()

	loader := &providers.ConfigLoader{}
	err := loader.SaveDatasourceUID(context.Background(), "tempo", "tempo-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple config files loaded")

	userCfg, err := internalconfig.Load(context.Background(), internalconfig.ExplicitConfigFile(userFile))
	require.NoError(t, err)
	assert.Empty(t, userCfg.Contexts["default"].Datasources["tempo"])

	localCfg, err := internalconfig.Load(context.Background(), internalconfig.ExplicitConfigFile(localFile))
	require.NoError(t, err)
	assert.Empty(t, localCfg.Contexts["default"].Datasources["tempo"])
}

// TestConfigLoader_SaveProviderConfig verifies AC-6: save and reload round-trip.
func TestConfigLoader_SaveProviderConfig(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	err := loader.SaveProviderConfig(context.Background(), "synth", "sm-metrics-datasource-uid", "abc123")
	require.NoError(t, err)

	// Reload and verify value persists.
	got, _, err := loader.LoadProviderConfig(context.Background(), "synth")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "abc123", got["sm-metrics-datasource-uid"])
}

// TestConfigLoader_SaveProviderConfig_ExistingProvider verifies that saving a key
// to an already-configured provider preserves other keys.
func TestConfigLoader_SaveProviderConfig_ExistingProvider(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    providers:
      synth:
        sm-url: https://file.sm
        sm-token: tok
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	err := loader.SaveProviderConfig(context.Background(), "synth", "sm-metrics-datasource-uid", "uid-xyz")
	require.NoError(t, err)

	got, _, err := loader.LoadProviderConfig(context.Background(), "synth")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "uid-xyz", got["sm-metrics-datasource-uid"])
	assert.Equal(t, "https://file.sm", got["sm-url"])
	assert.Equal(t, "tok", got["sm-token"])
}

// TestConfigLoader_LoadFullConfig verifies AC-7: returns non-nil *config.Config.
func TestConfigLoader_LoadFullConfig(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	cfg, err := loader.LoadFullConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "default", cfg.CurrentContext)
}

// TestConfigLoader_LoadGrafanaConfig_PersistsRefreshedTokens verifies that
// LoadGrafanaConfig wires SetOnRefresh so that a token refresh persists the
// new tokens back to the config file on disk.
func TestConfigLoader_LoadGrafanaConfig_PersistsRefreshedTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/v1/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_refreshed",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_refreshed",
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

	cfgFile := writeConfigFile(t, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_expiring
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 123
current-context: default
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	restCfg, err := loader.LoadGrafanaConfig(context.Background())
	require.NoError(t, err)

	// Trigger a request through the REST config transport to force a refresh.
	rt := restCfg.WrapTransport(http.DefaultTransport)
	client := &http.Client{Transport: rt}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Re-read the config file and verify the refreshed tokens were persisted.
	raw, err := os.ReadFile(cfgFile)
	require.NoError(t, err)
	contents := string(raw)
	assert.Contains(t, contents, "gat_refreshed")
	assert.Contains(t, contents, "gar_refreshed")
	assert.Contains(t, contents, "2099-01-01T00:00:00Z")
	assert.Contains(t, contents, "2099-02-01T00:00:00Z")
}

func TestLoadGrafanaConfig_PersistsRefreshToLocalOAuthLayer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/v1/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_refreshed_local",
					"expires_at":         "2099-01-01T00:00:00Z",
					"refresh_token":      "gar_refreshed_local",
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

	homeDir := t.TempDir()
	xdgDir := t.TempDir()
	workDir := t.TempDir()

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdg.Reload()
	t.Chdir(workDir)

	userFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	localFile := filepath.Join(workDir, ".gcx.yaml")

	writeFile(t, userFile, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_user_old
      oauth-refresh-token: gar_user_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 123
current-context: default
`)
	writeFile(t, localFile, `
contexts:
  default:
    grafana:
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_local_old
      oauth-refresh-token: gar_local_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
`)

	loader := &providers.ConfigLoader{}

	restCfg, err := loader.LoadGrafanaConfig(context.Background())
	require.NoError(t, err)

	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	localRaw, err := os.ReadFile(localFile)
	require.NoError(t, err)
	localContents := string(localRaw)
	assert.Contains(t, localContents, "gat_refreshed_local")
	assert.Contains(t, localContents, "gar_refreshed_local")

	userRaw, err := os.ReadFile(userFile)
	require.NoError(t, err)
	userContents := string(userRaw)
	assert.NotContains(t, userContents, "gat_refreshed_local")
	assert.NotContains(t, userContents, "gar_refreshed_local")
}

func TestLoadGrafanaConfig_PersistsRefreshToHighestContextLayer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/cli/v1/auth/refresh":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"token":              "gat_refreshed_local_ctx",
					"expires_at":         "2099-03-01T00:00:00Z",
					"refresh_token":      "gar_refreshed_local_ctx",
					"refresh_expires_at": "2099-04-01T00:00:00Z",
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

	homeDir := t.TempDir()
	xdgDir := t.TempDir()
	workDir := t.TempDir()

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	xdg.Reload()
	t.Chdir(workDir)

	userFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	localFile := filepath.Join(workDir, ".gcx.yaml")

	writeFile(t, userFile, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_user_old
      oauth-refresh-token: gar_user_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 123
current-context: default
`)
	writeFile(t, localFile, `
contexts:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
`)

	loader := &providers.ConfigLoader{}

	restCfg, err := loader.LoadGrafanaConfig(context.Background())
	require.NoError(t, err)

	client := &http.Client{Transport: restCfg.WrapTransport(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/test", nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	userRaw, err := os.ReadFile(userFile)
	require.NoError(t, err)
	userContents := string(userRaw)
	assert.NotContains(t, userContents, "gat_refreshed_local_ctx")
	assert.NotContains(t, userContents, "gar_refreshed_local_ctx")

	localRaw, err := os.ReadFile(localFile)
	require.NoError(t, err)
	localContents := string(localRaw)
	assert.Contains(t, localContents, "gat_refreshed_local_ctx")
	assert.Contains(t, localContents, "gar_refreshed_local_ctx")
}

// TestConfigLoader_LoadGrafanaConfig_BackwardCompat verifies AC-4: LoadGrafanaConfig
// still errors when no grafana server is configured.
func TestConfigLoader_LoadGrafanaConfig_BackwardCompat(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	_, err := loader.LoadGrafanaConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "grafana config is required")
}

// TestConfigLoader_LoadCloudConfig_FullRoundTrip tests the full happy-path:
// config file → LoadCloudConfig → mock GCOM server → populated CloudRESTConfig.
func TestConfigLoader_LoadCloudConfig_FullRoundTrip(t *testing.T) {
	wantStack := cloud.StackInfo{
		ID:                         42,
		Slug:                       "mystack",
		Name:                       "My Stack",
		URL:                        "https://mystack.grafana.net",
		AgentManagementInstanceID:  789,
		AgentManagementInstanceURL: "https://fleet.example.com",
	}

	srv := newMockGCOMServer(t, wantStack)
	defer srv.Close()

	// Use the full http:// URL — ResolveGCOMURL now preserves existing schemes.
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    cloud:
      token: "test-token"
      stack: "mystack"
      api-url: "`+srv.URL+`"
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	got, err := loader.LoadCloudConfig(context.Background())
	require.NoError(t, err)

	assert.Equal(t, "test-token", got.Token)
	assert.Equal(t, 42, got.Stack.ID)
	assert.Equal(t, "mystack", got.Stack.Slug)
	assert.Equal(t, "My Stack", got.Stack.Name)
	assert.Equal(t, 789, got.Stack.AgentManagementInstanceID)
	assert.Equal(t, "https://fleet.example.com", got.Stack.AgentManagementInstanceURL)
	assert.Equal(t, "default", got.Namespace)
}

func TestConfigLoader_SaveProviderConfig_ContextOverrideBeforeEnvVars(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  prod:
    providers:
      synth:
        sm-url: https://prod.sm
  staging:
    providers:
      synth:
        sm-url: https://staging.sm
current-context: prod
`)
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM-TOKEN", "env-sm-token")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	loader.SetContextName("staging")

	err := loader.SaveProviderConfig(context.Background(), "synth", "extra-key", "extra-val")
	require.NoError(t, err)

	// Reload and verify the save targeted the staging context.
	got, _, err := loader.LoadProviderConfig(context.Background(), "synth")
	require.NoError(t, err)
	assert.Equal(t, "https://staging.sm", got["sm-url"])
	assert.Equal(t, "extra-val", got["extra-key"])
	assert.Equal(t, "env-sm-token", got["sm-token"])
}

func TestConfigLoader_LoadFullConfig_ContextOverrideBeforeEnvVars(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  prod:
    grafana:
      server: https://prod.grafana.net
      token: prod-token
  staging:
    grafana:
      server: https://staging.grafana.net
      token: staging-token
current-context: prod
`)
	t.Setenv("GRAFANA_TOKEN", "env-token")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	loader.SetContextName("staging")

	cfg, err := loader.LoadFullConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "staging", cfg.CurrentContext)
	assert.Equal(t, "env-token", cfg.Contexts["staging"].Grafana.APIToken)
	assert.Equal(t, "https://staging.grafana.net", cfg.Contexts["staging"].Grafana.Server)
}

func TestConfigLoader_LoadGrafanaConfig_ContextOverrideBeforeEnvVars(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  prod:
    grafana:
      server: https://prod.grafana.net
      token: prod-token
  staging:
    grafana:
      server: https://staging.grafana.net
      token: staging-token
current-context: prod
`)
	t.Setenv("GRAFANA_TOKEN", "env-token")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	loader.SetContextName("staging")

	restCfg, err := loader.LoadGrafanaConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://staging.grafana.net", restCfg.Host)
	assert.Equal(t, "env-token", restCfg.BearerToken)
	assert.NotEqual(t, "https://prod.grafana.net", restCfg.Host)
}

func TestConfigLoader_LoadCloudConfig_ContextOverrideBeforeEnvVars(t *testing.T) {
	wantStack := cloud.StackInfo{ID: 7, Slug: "staging-stack", Name: "Staging"}
	srv := newMockGCOMServer(t, wantStack)
	defer srv.Close()

	cfgFile := writeConfigFile(t, `
contexts:
  prod:
    cloud:
      token: prod-token
      stack: prod-stack
      api-url: `+srv.URL+`
  staging:
    cloud:
      token: staging-token
      stack: staging-stack
      api-url: `+srv.URL+`
current-context: prod
`)
	t.Setenv("GRAFANA_CLOUD_TOKEN", "env-cloud-token")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	loader.SetContextName("staging")

	cloudCfg, err := loader.LoadCloudConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "env-cloud-token", cloudCfg.Token)
	assert.Equal(t, "staging-stack", cloudCfg.Stack.Slug)
	assert.Equal(t, 7, cloudCfg.Stack.ID)
	assert.NotEqual(t, "prod-stack", cloudCfg.Stack.Slug)
}
