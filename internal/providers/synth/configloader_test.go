package synth //nolint:testpackage // Tests need access to unexported configLoader for interface checks and direct construction.

import (
	"context"
	"os"
	"testing"

	"github.com/grafana/gcx/internal/providers/synth/smcfg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface satisfaction checks (AC: interface satisfaction).
// These fail at compile time if configLoader no longer satisfies the interfaces.
var (
	_ smcfg.Loader              = (*configLoader)(nil)
	_ smcfg.GrafanaConfigLoader = (*configLoader)(nil)
	_ smcfg.ConfigLoader        = (*configLoader)(nil)
	_ smcfg.DatasourceUIDSaver  = (*configLoader)(nil)
	_ smcfg.StatusLoader        = (*configLoader)(nil)
)

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

func newTestLoader(t *testing.T, cfgFile string) *configLoader {
	t.Helper()
	l := &configLoader{}
	l.SetConfigFile(cfgFile)
	return l
}

// TestConfigLoader_LoadSMConfig_FromContextDatasource verifies that
// LoadSMConfig returns the SM datasource UID configured under datasources.synth
// in the active context.
func TestConfigLoader_LoadSMConfig_FromContextDatasource(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    grafana:
      server: https://grafana.example.com
      token: test-token
      stack-id: 12345
    datasources:
      synth: sm-uid-from-config
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	cfg, datasourceUID, namespace, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "sm-uid-from-config", datasourceUID)
	// The namespace is derived from the stack-id, not the context name.
	assert.Equal(t, "stacks-12345", namespace)
	assert.NotEmpty(t, cfg.Host, "REST config should carry the Grafana server host")
}

// TestConfigLoader_LoadSMConfig_NoGrafanaServer verifies that LoadSMConfig
// fails clearly when no Grafana server is configured.
func TestConfigLoader_LoadSMConfig_NoGrafanaServer(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	_, _, _, err := l.LoadSMConfig(context.Background()) //nolint:dogsled // Only testing error return.
	require.Error(t, err)
}

// TestConfigLoader_SaveMetricsDatasourceUID_RoundTrip verifies that saving and
// reloading sm-metrics-datasource-uid round-trips correctly.
func TestConfigLoader_SaveMetricsDatasourceUID_RoundTrip(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	err := l.SaveMetricsDatasourceUID(context.Background(), "prom-123")
	require.NoError(t, err)

	// Reload via LoadConfig and verify the value persists.
	cfg, err := l.LoadConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	curCtx := cfg.GetCurrentContext()
	require.NotNil(t, curCtx)
	assert.Equal(t, "prom-123", curCtx.Providers["synth"]["sm-metrics-datasource-uid"])
}

// TestConfigLoader_LoadConfig_ReturnsConfig verifies that LoadConfig returns a
// non-nil *config.Config via LoadFullConfig.
func TestConfigLoader_LoadConfig_ReturnsConfig(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	cfg, err := l.LoadConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, "default", cfg.CurrentContext)
}
