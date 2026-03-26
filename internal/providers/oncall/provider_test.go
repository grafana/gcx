package oncall_test

import (
	"context"
	"os"
	"testing"

	"github.com/grafana/grafanactl/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeOncallConfig writes YAML to a temp config file and returns its path.
func writeOncallConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "grafanactl-oncall-*.yaml")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}

// TestDiscoverOnCallURL_EnvVar verifies that GRAFANA_PROVIDER_ONCALL_ONCALL_URL
// is surfaced as the "oncall-url" key via LoadProviderConfig (AC-8).
//
// The discoverOnCallURL method (on configLoader) calls LoadProviderConfig first
// and short-circuits to the returned "oncall-url" value before attempting plugin
// discovery. This test confirms the env var reaches LoadProviderConfig correctly.
func TestDiscoverOnCallURL_EnvVar(t *testing.T) {
	t.Setenv("GRAFANA_PROVIDER_ONCALL_ONCALL_URL", "https://oncall.example.com")

	cfgFile := writeOncallConfig(t, `
contexts:
  default: {}
current-context: default
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	providerCfg, _, err := loader.LoadProviderConfig(context.Background(), "oncall")
	require.NoError(t, err)
	assert.Equal(t, "https://oncall.example.com", providerCfg["oncall-url"],
		"GRAFANA_PROVIDER_ONCALL_ONCALL_URL must surface as oncall-url in provider config")
}

// TestDiscoverOnCallURL_NoEnvFallsToDiscovery verifies that when no URL is
// configured, the oncall-url key is absent (so plugin discovery fallback
// is triggered) (AC-9).
func TestDiscoverOnCallURL_NoEnvFallsToDiscovery(t *testing.T) {
	cfgFile := writeOncallConfig(t, `
contexts:
  default: {}
current-context: default
`)

	loader := &providers.ConfigLoader{}
	loader.SetConfigFile(cfgFile)

	providerCfg, _, err := loader.LoadProviderConfig(context.Background(), "oncall")
	require.NoError(t, err)
	assert.Empty(t, providerCfg["oncall-url"],
		"without env var, oncall-url must be absent so plugin discovery fallback is triggered")
}
