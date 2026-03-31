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
