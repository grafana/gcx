package assistant_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/assistant"
	"github.com/grafana/gcx/internal/providers"
	assistantcmd "github.com/grafana/gcx/internal/providers/assistant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAssistantClientOptionsUsesSelectedGrafanaAuth(t *testing.T) {
	t.Run("explicit token ignores stale OAuth fields", func(t *testing.T) {
		opts, err := resolveAssistantOptions(t, `
      auth-method: token
      token: selected-token
      proxy-endpoint: https://assistant.example.invalid
      oauth-token: stale-access-token
      oauth-refresh-token: stale-refresh-token
      user: stale-user
      password: stale-password`)
		require.NoError(t, err)
		assert.Equal(t, "selected-token", opts.Token)
		assert.Empty(t, opts.APIEndpoint)
		assert.Nil(t, opts.TokenRefresher)
	})

	t.Run("explicit OAuth accepts refresh-only and ignores stale token", func(t *testing.T) {
		opts, err := resolveAssistantOptions(t, `
      auth-method: oauth
      proxy-endpoint: https://assistant.example.invalid
      oauth-refresh-token: selected-refresh-token
      token: stale-service-account-token`)
		require.NoError(t, err)
		assert.Empty(t, opts.Token)
		assert.Equal(t, "https://assistant.example.invalid", opts.APIEndpoint)
		assert.NotNil(t, opts.TokenRefresher)
	})

	t.Run("explicit Basic fails unsupported before stale token can be used", func(t *testing.T) {
		_, err := resolveAssistantOptions(t, `
      auth-method: basic
      user: selected-user
      password: selected-password
      token: stale-service-account-token`)
		require.ErrorContains(t, err, `selected auth-method "basic" is not supported`)
	})

	t.Run("explicit mTLS fails unsupported before stale token can be used", func(t *testing.T) {
		certPath := filepath.Join(t.TempDir(), "client.pem")
		keyPath := filepath.Join(t.TempDir(), "client-key.pem")
		require.NoError(t, os.WriteFile(certPath, []byte("test certificate"), 0o600))
		require.NoError(t, os.WriteFile(keyPath, []byte("test private key"), 0o600))

		_, err := resolveAssistantOptions(t, fmt.Sprintf(`
      auth-method: mtls
      token: stale-service-account-token
      tls:
        cert-file: %s
        key-file: %s`, certPath, keyPath))
		require.ErrorContains(t, err, `selected auth-method "mtls" is not supported`)
	})
}

func resolveAssistantOptions(t *testing.T, grafanaFields string) (assistant.ClientOptions, error) {
	t.Helper()
	for _, key := range []string{
		"GRAFANA_SERVER",
		"GRAFANA_TOKEN",
		"GRAFANA_USER",
		"GRAFANA_PASSWORD",
		"GRAFANA_PROXY_ENDPOINT",
		"GRAFANA_ORG_ID",
		"GRAFANA_STACK_ID",
		"GRAFANA_TLS_CERT_FILE",
		"GRAFANA_TLS_KEY_FILE",
	} {
		unsetEnv(t, key)
	}

	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := fmt.Sprintf(`version: 1
stacks:
  prod:
    grafana:
      server: https://example.grafana.net
      stack-id: 12345%s
contexts:
  prod:
    stack: prod
current-context: prod
`, grafanaFields)
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(path)
	return assistantcmd.ResolveAssistantClientOptions(t.Context(), loader, 30, "test-agent")
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	// t.Setenv records and restores the original state; the explicit unset is
	// required because an empty noncredential variable is still an override.
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}
