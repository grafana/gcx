package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/gcx/internal/cloud"
	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/testutils"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigLoader_BindFlags_OnlyBindsConfig is a regression test for the
// duplicate `--context` flag binding that silently overrode the root command's
// `--context` value at the provider level. BindFlags must register only
// `--config`; `--context` is owned by the root command and threaded into
// context.Context via PersistentPreRun.
func TestConfigLoader_BindFlags_OnlyBindsConfig(t *testing.T) {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	loader := &providers.ConfigLoader{}
	loader.BindFlags(flags)

	require.NotNil(t, flags.Lookup("config"), "BindFlags must bind --config")
	assert.Nil(t, flags.Lookup("context"), "BindFlags must NOT bind --context (it is a root-level global flag)")
}

func TestConfigLoader_ConfigFilePrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())

	writeProviderConfig := func(account string) string {
		t.Helper()
		return writeConfigFile(t, `
version: 1
stacks:
  default:
    providers:
      routing:
        account: `+account+`
contexts:
  default:
    stack: default
current-context: default
`)
	}

	envFile := writeProviderConfig("env")
	contextFile := writeProviderConfig("context")
	boundFile := writeProviderConfig("bound")
	t.Setenv(internalconfig.ConfigFileEnvVar, envFile)

	ctx := internalconfig.ContextWithConfigFile(context.Background(), contextFile)
	loader := &providers.ConfigLoader{}
	got, _, err := loader.LoadProviderConfig(ctx, "routing")
	require.NoError(t, err)
	assert.Equal(t, "context", got["account"])

	loader.SetConfigFile(boundFile)
	got, _, err = loader.LoadProviderConfig(ctx, "routing")
	require.NoError(t, err)
	assert.Equal(t, "bound", got["account"])
}

func TestConfigLoader_ContextConfigFileRoutesDirectSnapshot(t *testing.T) {
	selectedFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    providers:
      synth:
        sm-url: https://selected.sm.invalid
        sm-token: selected-token
contexts:
  default:
    stack: default
current-context: default
`)
	wrongFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    providers:
      synth:
        sm-url: https://wrong.sm.invalid
        sm-token: wrong-token
contexts:
  default:
    stack: default
current-context: default
`)
	t.Setenv(internalconfig.ConfigFileEnvVar, wrongFile)

	ctx := internalconfig.ContextWithConfigFile(context.Background(), selectedFile)
	loader := &providers.ConfigLoader{}
	snapshot, err := loader.LoadDirectProviderSnapshot(ctx, providers.DirectProviderPolicy{
		ProviderName:  "synth",
		EndpointKeys:  []string{"sm-url"},
		CredentialEnv: "GRAFANA_PROVIDER_SYNTH_SM_TOKEN",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://selected.sm.invalid", snapshot.ProviderConfig["sm-url"])
	assert.Equal(t, "selected-token", snapshot.ProviderConfig["sm-token"])
}

func TestConfigLoader_BlankProviderCredentialEnvironment(t *testing.T) {
	cfgFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    providers:
      synth:
        sm-token: stored-sm-token
        sm-metrics-datasource-uid: stored-uid
contexts:
  default:
    stack: default
current-context: default
`)
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_TOKEN", " \t\n ")
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_METRICS_DATASOURCE_UID", "")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	got, _, err := loader.LoadProviderConfig(context.Background(), "synth")
	require.NoError(t, err)
	assert.Equal(t, "stored-sm-token", got["sm-token"])
	assert.Empty(t, got["sm-metrics-datasource-uid"],
		"blank non-secret provider environment values must retain their override semantics")
}

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

func isolateConfigSources(t *testing.T) (string, string, string) {
	t.Helper()
	homeDir := t.TempDir()
	xdgDir := t.TempDir()
	workDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	t.Setenv(internalconfig.ConfigFileEnvVar, "")
	t.Chdir(workDir)
	return homeDir, xdgDir, workDir
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
	assert.Contains(t, err.Error(), "context has no cloud auth")
}

func TestConfigLoader_LoadCloudConfig_MissingStack(t *testing.T) {
	cfgFile := writeConfigFile(t, `
version: 1
cloud:
  grafana-com:
    token: "my-token"
contexts:
  default:
    cloud: grafana-com
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
	// by ResolveCloudAPIURL as "https://", so we can't use the test server's plain
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
	assert.NotContains(t, err.Error(), "no cloud auth")
	assert.NotContains(t, err.Error(), "cloud stack is not configured")
}

// TestConfigLoader_LoadCloudConfig_GCOMCallAttempted verifies that when token and
// stack are configured, LoadCloudConfig actually attempts to call the GCOM API
// (the error is a network error, not a validation error).
func TestConfigLoader_LoadCloudConfig_GCOMCallAttempted(t *testing.T) {
	srv := newMockGCOMServer(t, cloud.StackInfo{ID: 42, Slug: "mystack"})
	defer srv.Close()

	// ResolveCloudAPIURL prepends "https://"; our test server is HTTP only. We
	// write api-url without the scheme so ResolveCloudAPIURL adds "https://".
	// This means the connection will fail at TLS, proving the GCOM call
	// was attempted (rather than a validation failure).
	cfgFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    slug: "mystack"
cloud:
  grafana-com:
    token: "test-token"
    api-url: "`+srv.URL[len("http://"):]+`"
contexts:
  default:
    stack: default
    cloud: grafana-com
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	_, err := loader.LoadCloudConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get stack info")
	assert.NotContains(t, err.Error(), "no cloud auth")
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
version: 1
stacks:
  default:
    providers:
      synth:
        sm-url: https://file.sm
contexts:
  default:
    stack: default
current-context: default
`,
			providerName: "synth",
			wantConfig:   map[string]string{"sm-url": "https://file.sm"},
		},
		{
			// AC-3: env var takes precedence over config file
			name: "env_var_overrides_config_file",
			configYAML: `
version: 1
stacks:
  default:
    providers:
      synth:
        sm-url: https://file.sm
contexts:
  default:
    stack: default
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

func TestLoadDirectProviderSnapshot_RejectsAutoLocalCredentialDestinationsBeforeNetwork(t *testing.T) {
	homeDir, _, workDir := isolateConfigSources(t)
	var attackerRequests atomic.Int32
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attackerRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(attacker.Close)

	userFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	writeFile(t, userFile, `
version: 1
stacks:
  user-stack:
    slug: victim-stack
cloud:
  grafana-com:
    token: user-cloud-token
    api-url: https://grafana.com
    oauth-url: https://grafana.com
contexts:
  user:
    stack: user-stack
    cloud: grafana-com
current-context: user
`)
	localFile := filepath.Join(workDir, internalconfig.LocalConfigFileName)
	writeFile(t, localFile, `
version: 1
stacks:
  repository:
    slug: victim-stack
    grafana:
      server: https://repository.invalid
      token: repository-token
    providers:
      faro:
        faro-api-url: `+attacker.URL+`
      synth:
        sm-url: `+attacker.URL+`
      adaptive:
        logs-tenant-id: "123"
      k6:
        api-domain: `+attacker.URL+`
contexts:
  repository:
    stack: repository
    cloud: grafana-com
current-context: repository
`)

	tests := []providers.DirectProviderPolicy{
		{ProviderName: "faro", EndpointKeys: []string{"faro-api-url"}, CredentialEnv: "GRAFANA_CLOUD_TOKEN", RejectAutoLocal: true},
		{ProviderName: "synth", EndpointKeys: []string{"sm-url"}, CredentialEnv: "GRAFANA_PROVIDER_SYNTH_SM_TOKEN", RejectAutoLocal: true},
		{ProviderName: "adaptive", EndpointKeys: []string{"logs-tenant-url"}, CredentialEnv: "GRAFANA_CLOUD_TOKEN", RejectAutoLocal: true},
		{ProviderName: "k6", EndpointKeys: []string{"api-domain"}, CredentialEnv: "GRAFANA_TOKEN", RequireGrafana: true},
	}
	for _, policy := range tests {
		t.Run(policy.ProviderName, func(t *testing.T) {
			loader := &providers.ConfigLoader{}
			_, err := loader.LoadDirectProviderSnapshot(context.Background(), policy)
			require.ErrorContains(t, err, "auto-discovered repository config")
			assert.Contains(t, err.Error(), "--config")
		})
	}
	assert.Zero(t, attackerRequests.Load())
}

func TestLoadDirectProviderSnapshot_RejectsAutoLocalEnvironmentCredentialBeforeNetwork(t *testing.T) {
	_, _, workDir := isolateConfigSources(t)
	localFile := filepath.Join(workDir, internalconfig.LocalConfigFileName)
	writeFile(t, localFile, `
version: 1
stacks:
  repository:
    providers:
      synth:
        sm-url: https://repository.invalid
contexts:
  repository:
    stack: repository
current-context: repository
`)
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_TOKEN", "runtime-secret")

	loader := &providers.ConfigLoader{}
	_, err := loader.LoadDirectProviderSnapshot(t.Context(), providers.DirectProviderPolicy{
		ProviderName:  "synth",
		EndpointKeys:  []string{"sm-url"},
		CredentialEnv: "GRAFANA_PROVIDER_SYNTH_SM_TOKEN",
	})
	require.Error(t, err)
	var rejected internalconfig.CredentialRejectedError
	require.ErrorAs(t, err, &rejected)
	assert.Contains(t, err.Error(), "before network use")
	assert.Contains(t, err.Error(), localFile)
}

func TestLoadDirectProviderSnapshot_EndpointEnvironmentRequiresMatchingCredential(t *testing.T) {
	configFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    slug: test-stack
    grafana:
      server: https://stack.grafana.net
      token: stored-grafana-token
      stack-id: 12345
    providers:
      faro:
        faro-api-url: https://stored-faro.invalid
      synth:
        sm-url: https://stored-sm.invalid
        sm-token: stored-sm-token
      adaptive:
        logs-tenant-url: https://stored-logs.invalid
        logs-tenant-id: "123"
      k6:
        api-domain: https://stored-k6.invalid
cloud:
  grafana-com:
    token: stored-cloud-token
    api-url: https://grafana.com
    oauth-url: https://grafana.com
contexts:
  default:
    stack: default
    cloud: grafana-com
current-context: default
`)
	tests := []struct {
		name          string
		policy        providers.DirectProviderPolicy
		endpointEnv   string
		credentialEnv string
	}{
		{"faro", providers.DirectProviderPolicy{ProviderName: "faro", EndpointKeys: []string{"faro-api-url"}, CredentialEnv: "GRAFANA_CLOUD_TOKEN"}, "GRAFANA_PROVIDER_FARO_FARO_API_URL", "GRAFANA_CLOUD_TOKEN"},
		{"synth", providers.DirectProviderPolicy{ProviderName: "synth", EndpointKeys: []string{"sm-url"}, CredentialEnv: "GRAFANA_PROVIDER_SYNTH_SM_TOKEN"}, "GRAFANA_PROVIDER_SYNTH_SM_URL", "GRAFANA_PROVIDER_SYNTH_SM_TOKEN"},
		{"adaptive", providers.DirectProviderPolicy{ProviderName: "adaptive", EndpointKeys: []string{"logs-tenant-url"}, CredentialEnv: "GRAFANA_CLOUD_TOKEN"}, "GRAFANA_PROVIDER_ADAPTIVE_LOGS_TENANT_URL", "GRAFANA_CLOUD_TOKEN"},
		{"k6", providers.DirectProviderPolicy{ProviderName: "k6", EndpointKeys: []string{"api-domain"}, CredentialEnv: "GRAFANA_TOKEN", RequireGrafana: true}, "GRAFANA_PROVIDER_K6_API_DOMAIN", "GRAFANA_TOKEN"},
	}
	for _, tt := range tests {
		t.Run(tt.name+" rejects destination-only override", func(t *testing.T) {
			t.Setenv(tt.endpointEnv, "https://attacker.invalid")
			t.Setenv(tt.credentialEnv, "")
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(configFile)
			_, err := loader.LoadDirectProviderSnapshot(context.Background(), tt.policy)
			require.ErrorContains(t, err, "without a matching runtime credential")
			assert.Contains(t, err.Error(), tt.credentialEnv)
		})
		t.Run(tt.name+" accepts paired runtime credential", func(t *testing.T) {
			t.Setenv(tt.endpointEnv, "https://runtime-authorized.invalid")
			t.Setenv(tt.credentialEnv, "runtime-token")
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(configFile)
			snapshot, err := loader.LoadDirectProviderSnapshot(context.Background(), tt.policy)
			require.NoError(t, err)
			assert.True(t, snapshot.EndpointOverriddenByEnvironment(tt.policy.EndpointKeys[0]))
			assert.Equal(t, "https://runtime-authorized.invalid", snapshot.ProviderConfig[tt.policy.EndpointKeys[0]])
		})
	}
}

func TestLoadDirectProviderSnapshot_EndpointPolicyFailsClosedWithoutCredentialEnvironment(t *testing.T) {
	loader := &providers.ConfigLoader{}
	_, err := loader.LoadDirectProviderSnapshot(context.Background(), providers.DirectProviderPolicy{
		ProviderName: "future-provider",
		EndpointKeys: []string{"api-url"},
	})
	require.ErrorContains(t, err, "requires a runtime credential environment variable")
}

func TestLoadDirectProviderSnapshot_ExplicitLocalConfigIsTrusted(t *testing.T) {
	_, _, workDir := isolateConfigSources(t)
	localFile := filepath.Join(workDir, internalconfig.LocalConfigFileName)
	writeFile(t, localFile, `
version: 1
stacks:
  default:
    providers:
      faro:
        faro-api-url: https://explicit-faro.invalid
cloud:
  grafana-com:
    token: explicit-cloud-token
    api-url: https://grafana.com
    oauth-url: https://grafana.com
contexts:
  default:
    stack: default
    cloud: grafana-com
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(localFile)
	snapshot, err := loader.LoadDirectProviderSnapshot(context.Background(), providers.DirectProviderPolicy{
		ProviderName:    "faro",
		EndpointKeys:    []string{"faro-api-url"},
		CredentialEnv:   "GRAFANA_CLOUD_TOKEN",
		RejectAutoLocal: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "https://explicit-faro.invalid", snapshot.ProviderConfig["faro-api-url"])
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

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

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

	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_CONFIG_HOME", xdgDir)

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
version: 1
stacks:
  default:
    providers:
      synth:
        sm-url: https://file.sm
        sm-token: tok
contexts:
  default:
    stack: default
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

func TestConfigLoader_SaveProviderConfig_DestinationChangeClearsCredential(t *testing.T) {
	cfgFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://grafana.invalid
    providers:
      synth:
        sm-url: https://old-sm.invalid
        sm-token: old-sm-token
contexts:
  default:
    stack: default
current-context: default
`)
	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	require.NoError(t, loader.SaveProviderConfig(context.Background(), "synth", "sm-url", "https://new-sm.invalid"))
	raw, err := os.ReadFile(cfgFile)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "https://new-sm.invalid")
	assert.NotContains(t, string(raw), "old-sm-token")
}

func TestConfigLoader_SaveProviderConfig_DoesNotPersistEnvOverrides(t *testing.T) {
	cfgFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    grafana:
      server: https://file.grafana.net
      token: file-token
      org-id: 1
    providers:
      synth:
        sm-url: https://file.sm
contexts:
  default:
    stack: default
current-context: default
`)
	t.Setenv("GRAFANA_SERVER", "https://env.grafana.net")
	t.Setenv("GRAFANA_TOKEN", "env-token")
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_URL", "https://env.sm")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	require.NoError(t, loader.SaveProviderConfig(context.Background(), "synth", "cached-key", "cached-value"))

	raw, err := internalconfig.Load(context.Background(), internalconfig.ExplicitConfigFile(cfgFile))
	require.NoError(t, err)
	stack := raw.Stacks["default"]
	require.NotNil(t, stack)
	require.NotNil(t, stack.Grafana)
	assert.Equal(t, "https://file.grafana.net", stack.Grafana.Server)
	assert.Equal(t, "file-token", stack.Grafana.APIToken)
	require.NotNil(t, stack.Providers["synth"])
	assert.Equal(t, "https://file.sm", stack.Providers["synth"]["sm-url"])
	assert.Equal(t, "cached-value", stack.Providers["synth"]["cached-key"])
}

func TestConfigLoader_SaveProviderConfig_RefusesImplicitLocalSource(t *testing.T) {
	homeDir, xdgDir, workDir := isolateConfigSources(t)
	localFile := filepath.Join(workDir, internalconfig.LocalConfigFileName)
	writeFile(t, localFile, `
version: 1
stacks:
  default: {}
contexts:
  default:
    stack: default
current-context: default
`)
	before, err := os.ReadFile(localFile)
	require.NoError(t, err)

	loader := &providers.ConfigLoader{}
	err = loader.SaveProviderConfig(context.Background(), "synth", "cached-key", "cached-value")
	require.ErrorIs(t, err, providers.ErrAutoLocalProviderWriteback)
	var localErr providers.AutoLocalProviderWritebackError
	require.ErrorAs(t, err, &localErr)
	assert.Equal(t, localFile, localErr.Path)
	after, err := os.ReadFile(localFile)
	require.NoError(t, err)
	assert.Equal(t, before, after)

	for _, path := range []string{
		filepath.Join(homeDir, ".config", "gcx", "config.yaml"),
		filepath.Join(xdgDir, "gcx", "config.yaml"),
	} {
		_, statErr := os.Stat(path)
		assert.True(t, os.IsNotExist(statErr), "provider save must not create %s", path)
	}
}

func TestConfigLoader_SaveProviderConfig_ExplicitLocalSourceIsAuthorized(t *testing.T) {
	_, _, workDir := isolateConfigSources(t)
	localFile := filepath.Join(workDir, internalconfig.LocalConfigFileName)
	writeFile(t, localFile, `
version: 1
stacks:
  default: {}
contexts:
  default:
    stack: default
current-context: default
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(localFile)
	require.NoError(t, loader.SaveProviderConfig(context.Background(), "synth", "cached-key", "cached-value"))

	raw, err := internalconfig.Load(context.Background(), internalconfig.ExplicitConfigFile(localFile))
	require.NoError(t, err)
	assert.Equal(t, "cached-value", raw.Stacks["default"].Providers["synth"]["cached-key"])
}

func TestConfigLoader_SaveProviderConfig_UsesGCXConfigSource(t *testing.T) {
	homeDir, _, workDir := isolateConfigSources(t)
	userFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	localFile := filepath.Join(workDir, internalconfig.LocalConfigFileName)
	explicitFile := filepath.Join(t.TempDir(), "explicit.yaml")
	for _, path := range []string{userFile, localFile, explicitFile} {
		writeFile(t, path, `
version: 1
stacks:
  default: {}
contexts:
  default:
    stack: default
current-context: default
`)
	}
	t.Setenv(internalconfig.ConfigFileEnvVar, explicitFile)

	loader := &providers.ConfigLoader{}
	require.NoError(t, loader.SaveProviderConfig(context.Background(), "synth", "cached-key", "cached-value"))

	for _, tc := range []struct {
		path      string
		wantValue string
	}{
		{path: userFile},
		{path: localFile},
		{path: explicitFile, wantValue: "cached-value"},
	} {
		raw, err := internalconfig.Load(context.Background(), internalconfig.ExplicitConfigFile(tc.path))
		require.NoError(t, err)
		assert.Equal(t, tc.wantValue, raw.Stacks["default"].Providers["synth"]["cached-key"])
	}
}

func TestConfigLoader_SaveProviderConfig_SkipsWhenNoConfigExists(t *testing.T) {
	homeDir, xdgDir, workDir := isolateConfigSources(t)

	loader := &providers.ConfigLoader{}
	require.NoError(t, loader.SaveProviderConfig(context.Background(), "synth", "cached-key", "cached-value"))

	for _, path := range []string{
		filepath.Join(homeDir, ".config", "gcx", "config.yaml"),
		filepath.Join(xdgDir, "gcx", "config.yaml"),
		filepath.Join(workDir, internalconfig.LocalConfigFileName),
	} {
		_, statErr := os.Stat(path)
		assert.True(t, os.IsNotExist(statErr), "provider save must not create %s", path)
	}
}

func TestConfigLoader_SaveProviderConfig_RefusesMultipleSources(t *testing.T) {
	homeDir, _, workDir := isolateConfigSources(t)
	userFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	localFile := filepath.Join(workDir, internalconfig.LocalConfigFileName)
	writeFile(t, userFile, `
version: 1
stacks:
  default:
    grafana:
      server: https://user.grafana.net
      token: user-token
contexts:
  default:
    stack: default
current-context: default
`)
	writeFile(t, localFile, `
version: 1
stacks:
  default:
    providers:
      synth:
        sm-url: https://local.sm
contexts:
  default:
    stack: default
current-context: default
`)
	userBefore, err := os.ReadFile(userFile)
	require.NoError(t, err)
	localBefore, err := os.ReadFile(localFile)
	require.NoError(t, err)

	loader := &providers.ConfigLoader{}
	err = loader.SaveProviderConfig(context.Background(), "synth", "cached-key", "cached-value")
	require.ErrorIs(t, err, providers.ErrAutoLocalProviderWriteback)
	var localErr providers.AutoLocalProviderWritebackError
	require.ErrorAs(t, err, &localErr)
	assert.Equal(t, localFile, localErr.Path)

	userAfter, err := os.ReadFile(userFile)
	require.NoError(t, err)
	localAfter, err := os.ReadFile(localFile)
	require.NoError(t, err)
	assert.Equal(t, userBefore, userAfter)
	assert.Equal(t, localBefore, localAfter)
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
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_expiring
      oauth-refresh-token: gar_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 123
contexts:
  default:
    stack: default
current-context: default
`)
	wrongFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_wrong_config
      oauth-refresh-token: gar_wrong_config
      oauth-token-expires-at: "2099-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-02-01T00:00:00Z"
      stack-id: 456
contexts:
  default:
    stack: default
current-context: default
`)
	t.Setenv(internalconfig.ConfigFileEnvVar, wrongFile)

	loader := &providers.ConfigLoader{}
	ctx := internalconfig.ContextWithConfigFile(context.Background(), cfgFile)

	restCfg, err := loader.LoadGrafanaConfig(ctx)
	require.NoError(t, err)

	// Trigger a request through the REST config transport to force a refresh.
	rt := restCfg.WrapTransport(http.DefaultTransport)
	client := &http.Client{Transport: rt}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/test", nil)
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

	wrongRaw, err := os.ReadFile(wrongFile)
	require.NoError(t, err)
	assert.Contains(t, string(wrongRaw), "gat_wrong_config")
	assert.NotContains(t, string(wrongRaw), "gat_refreshed")
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

	t.Chdir(workDir)

	userFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	localFile := filepath.Join(workDir, ".gcx.yaml")

	writeFile(t, userFile, `
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_user_old
      oauth-refresh-token: gar_user_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 123
contexts:
  default:
    stack: default
current-context: default
`)
	// Stack entries are atomic across layers: the local layer's entry is the
	// effective one wholesale, so it carries the full connection config, not
	// just the oauth fields.
	writeFile(t, localFile, `
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_local_old
      oauth-refresh-token: gar_local_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 123
contexts:
  default:
    stack: default
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

func TestLoadGrafanaConfig_PersistsRefreshToStackOwningLayer(t *testing.T) {
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

	t.Chdir(workDir)

	userFile := filepath.Join(homeDir, ".config", "gcx", "config.yaml")
	localFile := filepath.Join(workDir, ".gcx.yaml")

	writeFile(t, userFile, `
version: 1
stacks:
  default:
    grafana:
      server: "`+srv.URL+`"
      proxy-endpoint: "`+srv.URL+`"
      oauth-token: gat_user_old
      oauth-refresh-token: gar_user_old
      oauth-token-expires-at: "2020-01-01T00:00:00Z"
      oauth-refresh-expires-at: "2099-01-01T00:00:00Z"
      stack-id: 123
contexts:
  default:
    stack: default
current-context: default
`)
	// The local layer contributes only a thin context binding; the user layer
	// owns the stack entry. Stack entries are atomic across layers, so
	// refreshed tokens must land in the user file — writing them to the local
	// file would create a partial entry shadowing the user layer's stack.
	writeFile(t, localFile, `
version: 1
contexts:
  default:
    stack: default
    datasources:
      prometheus: local-prom
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
	assert.Contains(t, userContents, "gat_refreshed_local_ctx")
	assert.Contains(t, userContents, "gar_refreshed_local_ctx")

	localRaw, err := os.ReadFile(localFile)
	require.NoError(t, err)
	localContents := string(localRaw)
	assert.NotContains(t, localContents, "gat_refreshed_local_ctx")
	assert.NotContains(t, localContents, "gar_refreshed_local_ctx")
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
	assert.Contains(t, err.Error(), "context references no stack with grafana config")
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

	// Use the full http:// URL — ResolveCloudAPIURL now preserves existing schemes.
	cfgFile := writeConfigFile(t, `
version: 1
stacks:
  default:
    slug: "mystack"
cloud:
  grafana-com:
    token: "test-token"
    api-url: "`+srv.URL+`"
contexts:
  default:
    stack: default
    cloud: grafana-com
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
version: 1
stacks:
  prod:
    providers:
      synth:
        sm-url: https://prod.sm
  staging:
    providers:
      synth:
        sm-url: https://staging.sm
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
current-context: prod
`)
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_TOKEN", "env-sm-token")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	loader.SetContextName("staging")

	err := loader.SaveProviderConfig(context.Background(), "synth", "extra-key", "extra-val")
	require.NoError(t, err)

	// Inspect the raw file: the save targeted staging without changing the
	// persisted current-context or writing the environment-derived token.
	raw, err := internalconfig.Load(context.Background(), internalconfig.ExplicitConfigFile(cfgFile))
	require.NoError(t, err)
	assert.Equal(t, "prod", raw.CurrentContext)
	assert.Equal(t, "extra-val", raw.Stacks["staging"].Providers["synth"]["extra-key"])
	assert.NotContains(t, raw.Stacks["staging"].Providers["synth"], "sm-token")
	assert.NotContains(t, raw.Stacks["prod"].Providers["synth"], "extra-key")

	// The read path still applies the env override to staging in memory.
	got, _, err := loader.LoadProviderConfig(context.Background(), "synth")
	require.NoError(t, err)
	assert.Equal(t, "https://staging.sm", got["sm-url"])
	assert.Equal(t, "extra-val", got["extra-key"])
	assert.Equal(t, "env-sm-token", got["sm-token"])
}

func TestConfigLoader_LoadConfigTolerant_ContextOverrideBeforeEnvVars(t *testing.T) {
	cfgFile := writeConfigFile(t, `
version: 1
stacks:
  prod:
    grafana:
      server: https://prod.grafana.net
      token: prod-token
      org-id: 1
  staging:
    grafana:
      server: https://staging.grafana.net
      token: staging-token
      org-id: 1
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
current-context: prod
`)
	t.Setenv("GRAFANA_TOKEN", "env-token")

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)
	loader.SetContextName("staging")

	cfg, err := loader.LoadConfigTolerant(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "staging", cfg.CurrentContext)
	assert.Equal(t, "env-token", cfg.Contexts["staging"].Grafana.APIToken)
	assert.Equal(t, "prod-token", cfg.Contexts["prod"].Grafana.APIToken)
}

func TestConfigLoader_LoadConfig_ValidatesSelectedContext(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		wantErr    string
	}{
		{
			name: "valid selected context ignores invalid current context",
			configYAML: `
version: 1
stacks:
  staging:
    grafana:
      server: https://staging.grafana.net
      org-id: 1
contexts:
  prod:
    stack: missing
  staging:
    stack: staging
current-context: prod
`,
		},
		{
			name: "invalid selected context is rejected",
			configYAML: `
version: 1
stacks:
  prod:
    grafana:
      server: https://prod.grafana.net
      org-id: 1
contexts:
  prod:
    stack: prod
  staging:
    stack: missing
current-context: prod
`,
			wantErr: `stack "missing" is not defined`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfgFile := writeConfigFile(t, tc.configYAML)
			loader := &providers.ConfigLoader{}
			loader.SetConfigFile(cfgFile)
			loader.SetContextName("staging")

			cfg, err := loader.LoadConfig(context.Background())
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "staging", cfg.CurrentContext)
		})
	}
}

func TestConfigLoader_LoadFullConfig_ContextOverrideBeforeEnvVars(t *testing.T) {
	cfgFile := writeConfigFile(t, `
version: 1
stacks:
  prod:
    grafana:
      server: https://prod.grafana.net
      token: prod-token
  staging:
    grafana:
      server: https://staging.grafana.net
      token: staging-token
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
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
version: 1
stacks:
  prod:
    grafana:
      server: https://prod.grafana.net
      token: prod-token
  staging:
    grafana:
      server: https://staging.grafana.net
      token: staging-token
contexts:
  prod:
    stack: prod
  staging:
    stack: staging
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
version: 1
stacks:
  prod:
    slug: prod-stack
  staging:
    slug: staging-stack
cloud:
  prod-cloud:
    token: prod-token
    api-url: `+srv.URL+`
  staging-cloud:
    token: staging-token
    api-url: `+srv.URL+`
contexts:
  prod:
    stack: prod
    cloud: prod-cloud
  staging:
    stack: staging
    cloud: staging-cloud
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

// TestConfigLoader_NilLoaderBehavesLikeZeroValue pins the nil-receiver
// contract at the choke point: every provider Commands constructor accepts a
// *ConfigLoader that external callers (package tests in particular) pass as
// nil, and the resolved* fallbacks must treat that exactly like a zero-value
// loader instead of panicking. Removing the nil checks in
// resolvedContextName/resolvedConfigFile fails this test.
//
// SandboxConfigEnv rather than isolateConfigSources: the namespace assertion
// is hard-coded, so ambient GRAFANA_STACK_ID/GRAFANA_SERVER must be cleared.
func TestConfigLoader_NilLoaderBehavesLikeZeroValue(t *testing.T) {
	testutils.SandboxConfigEnv(t)
	cfgFile := writeConfigFile(t, `
version: 1
stacks:
  main:
    grafana:
      server: http://127.0.0.1:3000
      token: test-token
      stack-id: 11111
contexts:
  default:
    stack: main
current-context: default
`)
	t.Setenv(internalconfig.ConfigFileEnvVar, cfgFile)

	var nilLoader *providers.ConfigLoader
	got, err := nilLoader.LoadGrafanaConfig(context.Background())
	require.NoError(t, err)

	want, err := (&providers.ConfigLoader{}).LoadGrafanaConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want.Host, got.Host)
	assert.Equal(t, want.Namespace, got.Namespace)
	assert.Equal(t, "stacks-11111", got.Namespace)
}
