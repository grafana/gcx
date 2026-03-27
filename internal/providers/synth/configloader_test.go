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

// TestConfigLoader_LoadSMConfig_EnvVars verifies AC-5: LoadSMConfig resolves
// GRAFANA_PROVIDER_SYNTH_SM_URL and GRAFANA_PROVIDER_SYNTH_SM_TOKEN env vars.
func TestConfigLoader_LoadSMConfig_EnvVars(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default: {}
current-context: default
`)
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_URL", "https://sm.example.com")
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_TOKEN", "tok-env")

	l := newTestLoader(t, cfgFile)

	smURL, smToken, namespace, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://sm.example.com", smURL)
	assert.Equal(t, "tok-env", smToken)
	assert.Equal(t, "default", namespace)
}

// TestConfigLoader_LoadSMConfig_ConfigFile verifies that sm-url/sm-token from
// the config file are returned when no env vars are set.
func TestConfigLoader_LoadSMConfig_ConfigFile(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    providers:
      synth:
        sm-url: https://file.sm
        sm-token: tok-file
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	smURL, smToken, namespace, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://file.sm", smURL)
	assert.Equal(t, "tok-file", smToken)
	assert.Equal(t, "default", namespace)
}

// TestConfigLoader_LoadSMConfig_EnvVarOverridesFile verifies that env vars take
// precedence over config file values.
func TestConfigLoader_LoadSMConfig_EnvVarOverridesFile(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    providers:
      synth:
        sm-url: https://file.sm
        sm-token: tok-file
current-context: default
`)
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_URL", "https://env.sm")
	t.Setenv("GRAFANA_PROVIDER_SYNTH_SM_TOKEN", "tok-env")

	l := newTestLoader(t, cfgFile)

	smURL, smToken, _, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://env.sm", smURL)
	assert.Equal(t, "tok-env", smToken)
}

// TestConfigLoader_LoadSMConfig_MissingURL verifies that missing sm-url returns error.
func TestConfigLoader_LoadSMConfig_MissingURL(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    providers:
      synth:
        sm-token: tok
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	_, smToken, _, err := l.LoadSMConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SM URL not configured")
	assert.Empty(t, smToken)
}

// TestConfigLoader_LoadSMConfig_MissingToken verifies that missing sm-token returns error.
func TestConfigLoader_LoadSMConfig_MissingToken(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    providers:
      synth:
        sm-url: https://sm.example.com
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	smURL, _, _, err := l.LoadSMConfig(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SM token not configured")
	assert.Empty(t, smURL)
}

// TestConfigLoader_SaveMetricsDatasourceUID_RoundTrip verifies AC-6: saving and
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

// TestConfigLoader_LoadConfig_ReturnsConfig verifies AC-7: LoadConfig returns a
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
