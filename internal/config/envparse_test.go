package config_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrafanaTokenEnvironmentSelectsTokenWithoutChangingPersistedAuthMethod(t *testing.T) {
	t.Setenv("GRAFANA_TOKEN", "runtime-token")

	tests := map[string]config.GrafanaConfig{
		"OAuth": {
			AuthMethod:        "oauth",
			ProxyEndpoint:     "https://oauth-proxy.invalid",
			OAuthRefreshToken: "persisted-refresh",
		},
		"Basic": {
			AuthMethod: "basic",
			User:       "persisted-user",
			Password:   "persisted-password",
		},
		"mTLS": {
			AuthMethod: "mtls",
			TLS: &config.TLS{
				CertData: []byte("persisted-cert"),
				KeyData:  []byte("persisted-key"),
			},
		},
	}

	for name, grafana := range tests {
		t.Run(name, func(t *testing.T) {
			persistedMethod := grafana.AuthMethod
			grafana.Server = "https://grafana.invalid"
			grafana.OrgID = 1
			ctx := config.Context{Name: "prod", Grafana: &grafana}

			require.NoError(t, config.ParseEnvIntoContext(&ctx))
			assert.Equal(t, persistedMethod, ctx.Grafana.AuthMethod,
				"environment selection must not mutate the persisted selector")
			method, err := ctx.EffectiveGrafanaAuthMethod()
			require.NoError(t, err)
			assert.Equal(t, "token", method)
			require.NoError(t, ctx.Validate(context.Background()))

			restConfig, err := ctx.ToRESTConfig(context.Background())
			require.NoError(t, err)
			assert.Equal(t, "runtime-token", restConfig.BearerToken)
			assert.Empty(t, restConfig.Username)
			assert.Empty(t, restConfig.Password)
			assert.Empty(t, restConfig.CertData)
			assert.Empty(t, restConfig.KeyData)
			assert.False(t, restConfig.IsOAuthProxy())
		})
	}
}

func TestLoadGrafanaTokenEnvironmentDoesNotPersistDerivedSelector(t *testing.T) {
	t.Setenv("GRAFANA_TOKEN", "runtime-token")
	path := filepath.Join(t.TempDir(), "config.yaml")
	original := []byte(`version: 1
stacks:
  prod:
    grafana:
      server: https://grafana.invalid
      org-id: 1
      auth-method: oauth
contexts:
  prod:
    stack: prod
current-context: prod
`)
	require.NoError(t, os.WriteFile(path, original, 0o600))

	loaded, err := config.Load(context.Background(), config.ExplicitConfigFile(path), func(cfg *config.Config) error {
		return config.ParseEnvIntoContext(cfg.Contexts[cfg.CurrentContext])
	})
	require.NoError(t, err)
	method, err := loaded.Contexts["prod"].EffectiveGrafanaAuthMethod()
	require.NoError(t, err)
	assert.Equal(t, "token", method)
	assert.Equal(t, "oauth", loaded.Stacks["prod"].Grafana.AuthMethod)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, original, after, "runtime auth selection must not write through to the config file")
}

func TestGrafanaPasswordEnvironmentDoesNotSwitchPersistedAuthMethod(t *testing.T) {
	t.Setenv("GRAFANA_PASSWORD", "runtime-password")

	tests := map[string]config.GrafanaConfig{
		"OAuth remains OAuth": {
			AuthMethod:        "oauth",
			ProxyEndpoint:     "https://oauth-proxy.invalid",
			OAuthRefreshToken: "persisted-refresh",
			User:              "persisted-user",
		},
		"token remains token": {
			AuthMethod: "token",
			APIToken:   "persisted-token",
			User:       "persisted-user",
		},
		"mTLS remains mTLS": {
			AuthMethod: "mtls",
			User:       "persisted-user",
			TLS: &config.TLS{
				CertData: []byte("persisted-cert"),
				KeyData:  []byte("persisted-key"),
			},
		},
		"Basic rotates its password": {
			AuthMethod: "basic",
			User:       "persisted-user",
			Password:   "persisted-password",
		},
	}

	for name, grafana := range tests {
		t.Run(name, func(t *testing.T) {
			grafana.Server = "https://grafana.invalid"
			grafana.OrgID = 1
			ctx := config.Context{Name: "prod", Grafana: &grafana}

			require.NoError(t, config.ParseEnvIntoContext(&ctx))
			method, err := ctx.EffectiveGrafanaAuthMethod()
			require.NoError(t, err)
			assert.Equal(t, grafana.AuthMethod, method)
			assert.Equal(t, "runtime-password", ctx.Grafana.Password)
		})
	}
}

func TestParseEnvIntoContext_StringFields(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "https://example.com")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Equal(t, "https://example.com", ctx.Grafana.Server)
}

func TestParseEnvIntoContext_CloudFields(t *testing.T) {
	t.Setenv("GRAFANA_CLOUD_STACK", "mystack")
	t.Setenv("GRAFANA_CLOUD_TOKEN", "env-token")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Equal(t, "mystack", ctx.ResolveStackSlug())
	require.NotNil(t, ctx.CloudEntry)
	assert.Equal(t, "env-token", ctx.CloudEntry.Token)
}

func TestParseEnvIntoContext_Int64Fields(t *testing.T) {
	t.Setenv("GRAFANA_STACK_ID", "12345")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Equal(t, int64(12345), ctx.Grafana.StackID)
}

func TestParseEnvIntoContext_EmptyBoolSkipped(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "")

	opts, err := config.LoadCLIOptions()
	require.NoError(t, err)
	assert.False(t, opts.AutoApprove)
}

func TestParseEnvIntoContext_EmptyInt64Skipped(t *testing.T) {
	t.Setenv("GRAFANA_STACK_ID", "")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Equal(t, int64(0), ctx.Grafana.StackID)
}

func TestParseEnvIntoContext_EmptyStringIsSet(t *testing.T) {
	t.Setenv("GRAFANA_SERVER", "")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Empty(t, ctx.Grafana.Server)
}

func TestParseEnvIntoContext_NestedTLS(t *testing.T) {
	t.Setenv("GRAFANA_TLS_CERT_FILE", "/path/to/cert.pem")

	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	require.NotNil(t, ctx.Grafana.TLS)
	assert.Equal(t, "/path/to/cert.pem", ctx.Grafana.TLS.CertFile)
}

func TestParseEnvIntoContext_CleansUpEmptyTLS(t *testing.T) {
	// No TLS env vars set - TLS struct should be nil after cleanup.
	var ctx config.Context
	require.NoError(t, config.ParseEnvIntoContext(&ctx))
	assert.Nil(t, ctx.Grafana.TLS)
}

func TestLoadCLIOptions_BoolTrue(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "true")

	opts, err := config.LoadCLIOptions()
	require.NoError(t, err)
	assert.True(t, opts.AutoApprove)
}

func TestLoadCLIOptions_InvalidBoolErrors(t *testing.T) {
	t.Setenv("GCX_AUTO_APPROVE", "notabool")

	_, err := config.LoadCLIOptions()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GCX_AUTO_APPROVE")
}
