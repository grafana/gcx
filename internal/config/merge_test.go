package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeConfigs(t *testing.T) {
	tests := []struct {
		name string
		base config.Config
		over config.Config
		want config.Config
	}{
		{
			name: "higher layer overrides scalar fields",
			base: config.Config{CurrentContext: "base-ctx"},
			over: config.Config{CurrentContext: "over-ctx"},
			want: config.Config{CurrentContext: "over-ctx"},
		},
		{
			name: "higher layer does not erase with zero value",
			base: config.Config{CurrentContext: "base-ctx"},
			over: config.Config{CurrentContext: ""},
			want: config.Config{CurrentContext: "base-ctx"},
		},
		{
			name: "contexts merge by key",
			base: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"}},
				},
			},
			over: config.Config{
				Contexts: map[string]*config.Context{
					"staging": {Grafana: &config.GrafanaConfig{Server: "https://staging.grafana.net"}},
				},
			},
			want: config.Config{
				Contexts: map[string]*config.Context{
					"prod":    {Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"}},
					"staging": {Grafana: &config.GrafanaConfig{Server: "https://staging.grafana.net"}},
				},
			},
		},
		{
			name: "same context deep merges fields",
			base: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"}},
				},
			},
			over: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Cloud: &config.CloudConfig{Token: "cloud-token"}},
				},
			},
			want: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {
						Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"},
						Cloud:   &config.CloudConfig{Token: "cloud-token"},
					},
				},
			},
		},
		{
			name: "higher layer overrides field within same context",
			base: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://old.grafana.net", APIToken: "old-token"}},
				},
			},
			over: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://new.grafana.net"}},
				},
			},
			want: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://new.grafana.net", APIToken: "old-token"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.MergeConfigs(tt.base, tt.over)
			assert.Equal(t, tt.want.CurrentContext, got.CurrentContext)
			for name, wantCtx := range tt.want.Contexts {
				gotCtx, ok := got.Contexts[name]
				require.True(t, ok, "missing context %q", name)
				if wantCtx.Grafana != nil {
					require.NotNil(t, gotCtx.Grafana)
					assert.Equal(t, wantCtx.Grafana.Server, gotCtx.Grafana.Server)
					if wantCtx.Grafana.APIToken != "" {
						assert.Equal(t, wantCtx.Grafana.APIToken, gotCtx.Grafana.APIToken)
					}
				}
				if wantCtx.Cloud != nil {
					require.NotNil(t, gotCtx.Cloud)
					assert.Equal(t, wantCtx.Cloud.Token, gotCtx.Cloud.Token)
				}
			}
		})
	}
}

func TestLoadLayered_MergesThreeLayers(t *testing.T) {
	systemDir := t.TempDir()
	userDir := t.TempDir()
	localDir := t.TempDir()

	systemFile := filepath.Join(systemDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(systemFile), 0o755))
	require.NoError(t, os.WriteFile(systemFile, []byte(`
contexts:
  prod:
    grafana:
      server: https://prod.grafana.net
current-context: prod
`), 0o600))

	userFile := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o755))
	require.NoError(t, os.WriteFile(userFile, []byte(`
contexts:
  prod:
    grafana:
      token: user-token
  staging:
    grafana:
      server: https://staging.grafana.net
`), 0o600))

	localFile := filepath.Join(localDir, ".gcx.yaml")
	require.NoError(t, os.WriteFile(localFile, []byte(`
contexts:
  prod:
    cloud:
      token: local-cloud-token
`), 0o600))

	// Load each config independently and merge manually to validate merge logic.
	sysCfg, err := config.Load(t.Context(), config.ExplicitConfigFile(systemFile))
	require.NoError(t, err)

	usrCfg, err := config.Load(t.Context(), config.ExplicitConfigFile(userFile))
	require.NoError(t, err)

	lclCfg, err := config.Load(t.Context(), config.ExplicitConfigFile(localFile))
	require.NoError(t, err)

	// Merge in order: system → user → local.
	merged := config.MergeConfigs(sysCfg, usrCfg)
	merged = config.MergeConfigs(merged, lclCfg)

	// prod context should have: server from system, token from user, cloud from local.
	prodCtx := merged.Contexts["prod"]
	require.NotNil(t, prodCtx)
	assert.Equal(t, "https://prod.grafana.net", prodCtx.Grafana.Server)
	assert.Equal(t, "user-token", prodCtx.Grafana.APIToken)
	require.NotNil(t, prodCtx.Cloud)
	assert.Equal(t, "local-cloud-token", prodCtx.Cloud.Token)

	// staging context should exist (added by user layer).
	stagingCtx := merged.Contexts["staging"]
	require.NotNil(t, stagingCtx)
	assert.Equal(t, "https://staging.grafana.net", stagingCtx.Grafana.Server)

	// current-context: "prod" from system, not overridden (user/local don't set it).
	assert.Equal(t, "prod", merged.CurrentContext)
}

func TestMergeConfigs_DiagnosticsLayering(t *testing.T) {
	// User config enables the feature; local config omits the diagnostics block.
	// The user-layer value must survive.
	userCfg := config.Config{
		Diagnostics: &config.DiagnosticsConfig{AgentInvocationLog: true},
	}
	localCfg := config.Config{} // no Diagnostics block

	merged := config.MergeConfigs(userCfg, localCfg)

	require.NotNil(t, merged.Diagnostics, "diagnostics from user layer must survive")
	assert.True(t, merged.Diagnostics.AgentInvocationLog)
}

func TestMergeConfigs_CloudSettingsLayering(t *testing.T) {
	// User layer defines prod; local layer adds ops and switches current.
	// Envs merge by key and the higher layer's current wins.
	userCfg := config.Config{
		Cloud: &config.CloudSettings{
			Current: "prod",
			Envs:    map[string]*config.CloudConfig{"prod": {Token: "prod-token"}},
		},
	}
	localCfg := config.Config{
		Cloud: &config.CloudSettings{
			Current: "ops",
			Envs:    map[string]*config.CloudConfig{"ops": {Token: "ops-token"}},
		},
	}

	merged := config.MergeConfigs(userCfg, localCfg)

	require.NotNil(t, merged.Cloud)
	assert.Equal(t, "ops", merged.Cloud.Current, "higher layer current wins")
	assert.Equal(t, "prod-token", merged.Cloud.Envs["prod"].Token, "base env survives")
	assert.Equal(t, "ops-token", merged.Cloud.Envs["ops"].Token, "overlay env is added")
}

func TestMergeGrafanaConfig_OAuthAndProxyFields(t *testing.T) {
	tests := []struct {
		name string
		base config.GrafanaConfig
		over config.GrafanaConfig
		want config.GrafanaConfig
	}{
		{
			name: "overlay OAuthToken wins",
			base: config.GrafanaConfig{OAuthToken: "old"},
			over: config.GrafanaConfig{OAuthToken: "new"},
			want: config.GrafanaConfig{OAuthToken: "new"},
		},
		{
			name: "overlay OAuthRefreshToken and OAuthRefreshExpiresAt win",
			base: config.GrafanaConfig{OAuthRefreshToken: "old-refresh", OAuthRefreshExpiresAt: "2026-01-01T00:00:00Z"},
			over: config.GrafanaConfig{OAuthRefreshToken: "new-refresh", OAuthRefreshExpiresAt: "2027-01-01T00:00:00Z"},
			want: config.GrafanaConfig{OAuthRefreshToken: "new-refresh", OAuthRefreshExpiresAt: "2027-01-01T00:00:00Z"},
		},
		{
			name: "overlay ProxyEndpoint wins",
			base: config.GrafanaConfig{ProxyEndpoint: "http://old.proxy"},
			over: config.GrafanaConfig{ProxyEndpoint: "http://new.proxy"},
			want: config.GrafanaConfig{ProxyEndpoint: "http://new.proxy"},
		},
		{
			name: "zero overlay preserves base values for all five fields",
			base: config.GrafanaConfig{
				OAuthToken:            "tok",
				OAuthRefreshToken:     "rtok",
				OAuthTokenExpiresAt:   "2026-01-01T00:00:00Z",
				OAuthRefreshExpiresAt: "2026-06-01T00:00:00Z",
				ProxyEndpoint:         "http://proxy",
			},
			over: config.GrafanaConfig{},
			want: config.GrafanaConfig{
				OAuthToken:            "tok",
				OAuthRefreshToken:     "rtok",
				OAuthTokenExpiresAt:   "2026-01-01T00:00:00Z",
				OAuthRefreshExpiresAt: "2026-06-01T00:00:00Z",
				ProxyEndpoint:         "http://proxy",
			},
		},
		{
			name: "overlay OAuthTokenExpiresAt wins",
			base: config.GrafanaConfig{OAuthTokenExpiresAt: "2026-01-01T00:00:00Z"},
			over: config.GrafanaConfig{OAuthTokenExpiresAt: "2027-01-01T00:00:00Z"},
			want: config.GrafanaConfig{OAuthTokenExpiresAt: "2027-01-01T00:00:00Z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := config.Config{Contexts: map[string]*config.Context{
				"ctx": {Grafana: &tt.base},
			}}
			over := config.Config{Contexts: map[string]*config.Context{
				"ctx": {Grafana: &tt.over},
			}}
			got := config.MergeConfigs(base, over)
			gotGrafana := got.Contexts["ctx"].Grafana
			assert.Equal(t, tt.want.OAuthToken, gotGrafana.OAuthToken)
			assert.Equal(t, tt.want.OAuthRefreshToken, gotGrafana.OAuthRefreshToken)
			assert.Equal(t, tt.want.OAuthTokenExpiresAt, gotGrafana.OAuthTokenExpiresAt)
			assert.Equal(t, tt.want.OAuthRefreshExpiresAt, gotGrafana.OAuthRefreshExpiresAt)
			assert.Equal(t, tt.want.ProxyEndpoint, gotGrafana.ProxyEndpoint)
		})
	}
}

func TestMergeConfigs_DiagnosticsOverride(t *testing.T) {
	// Local config can override individual diagnostics fields.
	userCfg := config.Config{
		Diagnostics: &config.DiagnosticsConfig{AgentInvocationLog: true, LogDir: "/user/logs"},
	}
	localCfg := config.Config{
		Diagnostics: &config.DiagnosticsConfig{LogDir: "/local/logs"},
	}

	merged := config.MergeConfigs(userCfg, localCfg)

	require.NotNil(t, merged.Diagnostics)
	assert.True(t, merged.Diagnostics.AgentInvocationLog, "feature stays enabled from user layer")
	assert.Equal(t, "/local/logs", merged.Diagnostics.LogDir, "local override wins for LogDir")
}
