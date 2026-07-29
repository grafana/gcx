package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConfigPath(t *testing.T) {
	cfg := config.Config{
		Stacks: map[string]*config.StackConfig{
			"dev": {Grafana: &config.GrafanaConfig{Server: "https://dev.grafana.net"}},
		},
		Cloud: map[string]*config.CloudEntry{
			"grafana-com": {Token: "tok"},
		},
		Contexts: map[string]*config.Context{
			"dev": {Stack: "dev", Cloud: "grafana-com"},
		},
		CurrentContext: "dev",
	}
	cfg.Resolve()

	valid := []string{
		"stacks.dev.grafana.server",
		"stacks.dev.slug",
		"stacks.dev.providers.slo.org-id",
		"cloud.grafana-com.token",
		"cloud.grafana-com.api-url",
		"contexts.dev.stack",
		"contexts.dev.cloud",
		"contexts.dev.datasources.prometheus",
		"current-context",
		"resources.assume-server-dry-run",
		"diagnostics.telemetry",
		"version",
	}
	for _, path := range valid {
		t.Run("valid/"+path, func(t *testing.T) {
			got, err := config.ValidateConfigPath(cfg, path)
			require.NoError(t, err)
			assert.Equal(t, path, got, "valid paths pass through unchanged")
		})
	}

	invalid := []struct {
		path string
		hint string
	}{
		// Bare stack-owned paths are not routed; the error names the exact
		// absolute location using the current context's stack.
		{"grafana.server", "stacks.dev.grafana.server"},
		{"providers.slo.org-id", "stacks.dev.providers.slo.org-id"},
		{"slug", "stacks.dev.slug"},
		// Bare context-owned paths likewise.
		{"datasources.prometheus", "contexts.dev.datasources.prometheus"},
		{"stack", "contexts.dev.stack"},
		// Legacy datasource fields point at the datasources map.
		{"default-prometheus-datasource", "contexts.dev.datasources.prometheus"},
		{"default-loki-datasource", "contexts.dev.datasources.loki"},
		// cloud.<field> would silently create an entry named after the field.
		{"cloud.token", "cloud.<entry>.token"},
		{"cloud.oauth-token", "cloud.<entry>.oauth-token"},
		// Bare `cloud` (the old context-ref path) is ambiguous with the
		// top-level map.
		{"cloud", "cloud.<entry>."},
		// Unknown paths get the general grammar.
		{"nonsense.path", "top-level section"},
	}
	for _, tc := range invalid {
		t.Run("invalid/"+tc.path, func(t *testing.T) {
			_, err := config.ValidateConfigPath(cfg, tc.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.hint)
		})
	}

	t.Run("cloud error names the bound entry", func(t *testing.T) {
		_, err := config.ValidateConfigPath(cfg, "cloud.token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cloud.grafana-com")
	})

	t.Run("no current context uses placeholders", func(t *testing.T) {
		_, err := config.ValidateConfigPath(config.Config{}, "grafana.server")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stacks.<name>.grafana.server")
	})
}
