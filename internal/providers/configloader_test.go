package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/grafana/grafanactl/internal/cloud"
	"github.com/grafana/grafanactl/internal/providers"
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
	f, err := os.CreateTemp(t.TempDir(), "grafanactl-config-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
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
