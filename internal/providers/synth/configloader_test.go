package synth //nolint:testpackage // Tests need access to unexported configLoader for interface checks and direct construction.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/grafana/gcx/internal/config"
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

// TestConfigLoader_LoadSMConfig_AutoDiscovery verifies that sm-url is auto-discovered
// from plugin settings when not explicitly configured and a Grafana server is available.
func TestConfigLoader_LoadSMConfig_AutoDiscovery(t *testing.T) {
	const wantURL = "https://synthetic-monitoring-api-eu-west-2.grafana.net"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-synthetic-monitoring-app/settings" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonData": map[string]any{
					"apiHost": wantURL,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfgFile := writeConfigFile(t, `
contexts:
  default:
    grafana:
      server: `+srv.URL+`
      token: test-token
      stack-id: 12345
    providers:
      synth:
        sm-token: tok-test
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	smURL, smToken, _, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wantURL, smURL)
	assert.Equal(t, "tok-test", smToken)
}

// TestConfigLoader_LoadSMConfig_AutoDiscoveryPersistsToConfig verifies that an
// auto-discovered SM URL is saved to the config file for subsequent runs.
func TestConfigLoader_LoadSMConfig_AutoDiscoveryPersistsToConfig(t *testing.T) {
	const wantURL = "https://synthetic-monitoring-api-eu-west-2.grafana.net"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-synthetic-monitoring-app/settings" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonData": map[string]any{
					"apiHost": wantURL,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfgFile := writeConfigFile(t, `
contexts:
  default:
    grafana:
      server: `+srv.URL+`
      token: test-token
      stack-id: 12345
    providers:
      synth:
        sm-token: tok-test
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	// First call: auto-discovers and persists.
	smURL, _, _, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wantURL, smURL)

	// Verify persisted: reload config and check the value was saved.
	cfg, err := l.LoadConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wantURL, cfg.GetCurrentContext().Providers["synth"]["sm-url"])
}

// TestDiscoverSMURL_UsesGrafanaURL verifies that discoverSMURL uses GrafanaURL
// (the original Grafana server) rather than Host (which may be a proxy endpoint).
// This prevents a bug where OAuth proxy users hit the wrong endpoint.
func TestDiscoverSMURL_UsesGrafanaURL(t *testing.T) {
	const wantURL = "https://synthetic-monitoring-api-eu-west-2.grafana.net"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/plugins/grafana-synthetic-monitoring-app/settings" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonData": map[string]any{"apiHost": wantURL},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Simulate OAuth proxy mode: Host points to a proxy, GrafanaURL points to the real server.
	cfg := config.NamespacedRESTConfig{
		GrafanaURL: srv.URL,
	}
	cfg.Host = "https://proxy.example.com/api/cli/v1/proxy"

	got, err := discoverSMURL(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, wantURL, got)
}

// TestConfigLoader_LoadSMConfig_AutoDiscoveryFallsBackToError verifies that
// when auto-discovery fails (no Grafana server), a clear error is returned.
func TestConfigLoader_LoadSMConfig_AutoDiscoveryFallsBackToError(t *testing.T) {
	cfgFile := writeConfigFile(t, `
contexts:
  default:
    providers:
      synth:
        sm-token: tok
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	_, _, _, err := l.LoadSMConfig(context.Background()) //nolint:dogsled // Only testing error return.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SM URL not configured")
}

// TestConfigLoader_LoadSMConfig_ExplicitURLSkipsAutoDiscovery verifies that
// auto-discovery is not attempted when sm-url is already configured.
func TestConfigLoader_LoadSMConfig_ExplicitURLSkipsAutoDiscovery(t *testing.T) {
	// Server that would fail if called — proves discovery was skipped.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("auto-discovery should not be attempted when sm-url is configured")
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfgFile := writeConfigFile(t, `
contexts:
  default:
    providers:
      synth:
        sm-url: https://explicit.sm
        sm-token: tok-test
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	smURL, _, _, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "https://explicit.sm", smURL)
}

// TestConfigLoader_LoadSMConfig_TokenAutoDiscovery verifies that sm-token is
// auto-discovered via the SM register/install API when cloud config is available.
func TestConfigLoader_LoadSMConfig_TokenAutoDiscovery(t *testing.T) {
	const wantToken = "sm-access-token-abc123"

	// Mock SM API server that handles both plugin settings and register/install.
	smSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/register/install" && r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken": wantToken,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer smSrv.Close()

	// Mock GCOM server that returns stack info.
	gcomSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                12345,
			"slug":              "teststack",
			"regionSlug":        "eu-west-2",
			"hmInstancePromId":  67890,
			"hlInstanceId":      11111,
			"hmInstancePromUrl": "https://prom.example.com",
			"hlInstanceUrl":     "https://loki.example.com",
		})
	}))
	defer gcomSrv.Close()

	cfgFile := writeConfigFile(t, `
contexts:
  default:
    grafana:
      server: https://teststack.grafana.net
      token: test-token
      stack-id: 12345
    cloud:
      token: cloud-token-xyz
      stack: teststack
      api-url: `+gcomSrv.URL+`
    providers:
      synth:
        sm-url: `+smSrv.URL+`
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	smURL, smToken, _, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, smSrv.URL, smURL)
	assert.Equal(t, wantToken, smToken)
}

// TestConfigLoader_LoadSMConfig_TokenPersistsToConfig verifies that an
// auto-discovered SM token is saved to the config file for subsequent runs.
func TestConfigLoader_LoadSMConfig_TokenPersistsToConfig(t *testing.T) {
	const wantToken = "sm-access-token-persisted"

	smSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/register/install" {
			_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": wantToken})
			return
		}
		http.NotFound(w, r)
	}))
	defer smSrv.Close()

	gcomSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 12345, "slug": "teststack", "regionSlug": "eu",
			"hmInstancePromId": 1, "hlInstanceId": 2,
			"hmInstancePromUrl": "https://prom.example.com",
			"hlInstanceUrl":     "https://loki.example.com",
		})
	}))
	defer gcomSrv.Close()

	cfgFile := writeConfigFile(t, `
contexts:
  default:
    grafana:
      server: https://teststack.grafana.net
      token: test-token
      stack-id: 12345
    cloud:
      token: cloud-token
      stack: teststack
      api-url: `+gcomSrv.URL+`
    providers:
      synth:
        sm-url: `+smSrv.URL+`
current-context: default
`)

	l := newTestLoader(t, cfgFile)

	_, smToken, _, err := l.LoadSMConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wantToken, smToken)

	// Verify persisted.
	cfg, err := l.LoadConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wantToken, cfg.GetCurrentContext().Providers["synth"]["sm-token"])
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
