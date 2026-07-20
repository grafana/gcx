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
			name: "stacks merge by key",
			base: config.Config{
				Stacks: map[string]*config.StackConfig{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"}},
				},
			},
			over: config.Config{
				Stacks: map[string]*config.StackConfig{
					"staging": {Grafana: &config.GrafanaConfig{Server: "https://staging.grafana.net"}},
				},
			},
			want: config.Config{
				Stacks: map[string]*config.StackConfig{
					"prod":    {Grafana: &config.GrafanaConfig{Server: "https://prod.grafana.net"}},
					"staging": {Grafana: &config.GrafanaConfig{Server: "https://staging.grafana.net"}},
				},
			},
		},
		{
			name: "same context deep merges refs from both layers",
			base: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Stack: "prod"},
				},
			},
			over: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Cloud: "grafana-com"},
				},
			},
			want: config.Config{
				Contexts: map[string]*config.Context{
					"prod": {Stack: "prod", Cloud: "grafana-com"},
				},
			},
		},
		{
			name: "higher layer overrides field within same stack",
			base: config.Config{
				Stacks: map[string]*config.StackConfig{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://old.grafana.net", APIToken: "old-token"}},
				},
			},
			over: config.Config{
				Stacks: map[string]*config.StackConfig{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://new.grafana.net"}},
				},
			},
			want: config.Config{
				Stacks: map[string]*config.StackConfig{
					"prod": {Grafana: &config.GrafanaConfig{Server: "https://new.grafana.net", APIToken: "old-token"}},
				},
			},
		},
		{
			name: "cloud entries merge by key with field-level override",
			base: config.Config{
				Cloud: map[string]*config.CloudEntry{
					"grafana-com": {Token: "old-token", APIUrl: "https://grafana.com"},
				},
			},
			over: config.Config{
				Cloud: map[string]*config.CloudEntry{
					"grafana-com": {Token: "new-token"},
				},
			},
			want: config.Config{
				Cloud: map[string]*config.CloudEntry{
					"grafana-com": {Token: "new-token", APIUrl: "https://grafana.com"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := config.MergeConfigs(tt.base, tt.over)
			assert.Equal(t, tt.want.CurrentContext, got.CurrentContext)
			for name, wantStack := range tt.want.Stacks {
				gotStack, ok := got.Stacks[name]
				require.True(t, ok, "missing stack %q", name)
				require.NotNil(t, gotStack.Grafana)
				assert.Equal(t, wantStack.Grafana.Server, gotStack.Grafana.Server)
				if wantStack.Grafana.APIToken != "" {
					assert.Equal(t, wantStack.Grafana.APIToken, gotStack.Grafana.APIToken)
				}
			}
			for name, wantEntry := range tt.want.Cloud {
				gotEntry, ok := got.Cloud[name]
				require.True(t, ok, "missing cloud entry %q", name)
				assert.Equal(t, wantEntry.Token, gotEntry.Token)
				assert.Equal(t, wantEntry.APIUrl, gotEntry.APIUrl)
			}
			for name, wantCtx := range tt.want.Contexts {
				gotCtx, ok := got.Contexts[name]
				require.True(t, ok, "missing context %q", name)
				assert.Equal(t, wantCtx.Stack, gotCtx.Stack)
				assert.Equal(t, wantCtx.Cloud, gotCtx.Cloud)
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
version: 1
stacks:
  prod:
    grafana:
      server: https://prod.grafana.net
contexts:
  prod:
    stack: prod
current-context: prod
`), 0o600))

	userFile := filepath.Join(userDir, "gcx", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userFile), 0o755))
	require.NoError(t, os.WriteFile(userFile, []byte(`
version: 1
stacks:
  prod:
    grafana:
      token: user-token
  staging:
    grafana:
      server: https://staging.grafana.net
contexts:
  staging:
    stack: staging
`), 0o600))

	localFile := filepath.Join(localDir, ".gcx.yaml")
	require.NoError(t, os.WriteFile(localFile, []byte(`
version: 1
cloud:
  grafana-com:
    token: local-cloud-token
contexts:
  prod:
    cloud: grafana-com
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
	require.NotNil(t, prodCtx.Grafana)
	assert.Equal(t, "https://prod.grafana.net", prodCtx.Grafana.Server)
	assert.Equal(t, "user-token", prodCtx.Grafana.APIToken)
	require.NotNil(t, prodCtx.CloudEntry)
	assert.Equal(t, "local-cloud-token", prodCtx.CloudEntry.Token)

	// staging context should exist (added by user layer).
	stagingCtx := merged.Contexts["staging"]
	require.NotNil(t, stagingCtx)
	require.NotNil(t, stagingCtx.Grafana)
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

func TestMergeStacks_AssumeServerDryRunLastWins(t *testing.T) {
	tests := []struct {
		name string
		base []string
		over []string
		want []string
	}{
		{
			name: "higher layer replaces lower layer's list",
			base: []string{"a.grp", "shared.grp"},
			over: []string{"shared.grp", "b.grp"},
			want: []string{"shared.grp", "b.grp"},
		},
		{
			name: "explicit empty list clears lower layer's assertions",
			base: []string{"a.grp"},
			over: []string{},
			want: []string{},
		},
		{
			name: "unset in higher layer keeps lower layer's list",
			base: []string{"a.grp"},
			over: nil,
			want: []string{"a.grp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := config.Config{Stacks: map[string]*config.StackConfig{
				"stk": {Resources: &config.ResourcesConfig{AssumeServerDryRun: tt.base}},
			}}
			over := config.Config{Stacks: map[string]*config.StackConfig{
				"stk": {Resources: &config.ResourcesConfig{AssumeServerDryRun: tt.over}},
			}}

			merged := config.MergeConfigs(base, over)

			require.NotNil(t, merged.Stacks["stk"].Resources)
			assert.Equal(t, tt.want, merged.Stacks["stk"].Resources.AssumeServerDryRun)
		})
	}
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
			base := config.Config{Stacks: map[string]*config.StackConfig{
				"stk": {Grafana: &tt.base},
			}}
			over := config.Config{Stacks: map[string]*config.StackConfig{
				"stk": {Grafana: &tt.over},
			}}
			got := config.MergeConfigs(base, over)
			gotGrafana := got.Stacks["stk"].Grafana
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
