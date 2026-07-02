//nolint:testpackage // white-box testing: accesses unexported saveCloudConfig/applyCloudConfig.
package cloud

import (
	"context"
	"path/filepath"
	"testing"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveCloudConfigPreservesStack verifies that re-authenticating (which
// writes a fresh CloudConfig with only auth fields) does not drop a previously
// configured non-auth Stack selection on a per-context override.
func TestSaveCloudConfigPreservesStack(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)

	seed := config.Config{}
	seed.SetContext(config.DefaultContextName, true, config.Context{
		Cloud: &config.CloudConfig{
			Token:    "old-token",
			Stack:    "mystack",
			OAuthUrl: "https://old.example",
		},
	})
	require.NoError(t, config.Write(ctx, source, seed))

	configOpts := &cmdconfig.Options{ConfigFile: path}
	newCloud := &config.CloudConfig{
		Token:    "new-token",
		OAuthUrl: "https://grafana.com",
		APIUrl:   "https://grafana.com",
	}
	require.NoError(t, saveCloudConfig(ctx, configOpts, writeTarget{ctxName: config.DefaultContextName}, newCloud))

	got, err := config.Load(ctx, source)
	require.NoError(t, err)
	cloud := got.Contexts[config.DefaultContextName].Cloud
	assert.Equal(t, "mystack", cloud.Stack, "Stack must be preserved across re-auth")
	assert.Equal(t, "new-token", cloud.Token, "Token must be updated")
}

func TestApplyCloudConfig_EnvTarget(t *testing.T) {
	t.Run("creates env and sets current on first login", func(t *testing.T) {
		cfg := &config.Config{}
		dest, err := applyCloudConfig(cfg, writeTarget{envName: "prod"}, &config.CloudConfig{Token: "tok"})
		require.NoError(t, err)
		assert.Equal(t, `environment "prod"`, dest)
		require.NotNil(t, cfg.Cloud)
		assert.Equal(t, "prod", cfg.Cloud.Current, "first env configured becomes current")
		assert.Equal(t, "tok", cfg.Cloud.Envs["prod"].Token)
	})

	t.Run("adding an env never switches the current one", func(t *testing.T) {
		cfg := &config.Config{Cloud: &config.CloudSettings{
			Current: "prod",
			Envs:    map[string]*config.CloudConfig{"prod": {Token: "p"}},
		}}
		_, err := applyCloudConfig(cfg, writeTarget{envName: "ops"}, &config.CloudConfig{Token: "o"})
		require.NoError(t, err)
		assert.Equal(t, "prod", cfg.Cloud.Current)
		assert.Equal(t, "o", cfg.Cloud.Envs["ops"].Token)
	})
}

func TestApplyCloudConfig_ContextTarget(t *testing.T) {
	t.Run("writes the per-context override", func(t *testing.T) {
		cfg := &config.Config{Contexts: map[string]*config.Context{
			"pdc-dev": {Grafana: &config.GrafanaConfig{Server: "https://x.invalid"}},
		}}
		dest, err := applyCloudConfig(cfg, writeTarget{ctxName: "pdc-dev"}, &config.CloudConfig{Token: "cap"})
		require.NoError(t, err)
		assert.Equal(t, `context "pdc-dev"`, dest)
		assert.Equal(t, "cap", cfg.Contexts["pdc-dev"].Cloud.Token)
		assert.Nil(t, cfg.Cloud, "context target must not touch the top-level section")
	})

	t.Run("preserves an existing stack selection", func(t *testing.T) {
		cfg := &config.Config{Contexts: map[string]*config.Context{
			"pdc-dev": {Cloud: &config.CloudConfig{Token: "old", Stack: "mystack"}},
		}}
		_, err := applyCloudConfig(cfg, writeTarget{ctxName: "pdc-dev"}, &config.CloudConfig{Token: "new"})
		require.NoError(t, err)
		assert.Equal(t, "mystack", cfg.Contexts["pdc-dev"].Cloud.Stack)
		assert.Equal(t, "new", cfg.Contexts["pdc-dev"].Cloud.Token)
	})

	t.Run("errors when the context does not exist", func(t *testing.T) {
		cfg := &config.Config{Contexts: map[string]*config.Context{}}
		_, err := applyCloudConfig(cfg, writeTarget{ctxName: "missing"}, &config.CloudConfig{Token: "cap"})
		require.Error(t, err)
	})
}
