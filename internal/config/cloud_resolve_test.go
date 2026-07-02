package config_test

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestConfig_ResolveCloudEnvName(t *testing.T) {
	testCases := []struct {
		name string
		cfg  config.Config
		ctx  string
		want string
	}{
		{
			name: "no cloud section falls back to prod",
			cfg:  config.Config{},
			want: "prod",
		},
		{
			name: "explicit current wins",
			cfg: config.Config{Cloud: &config.CloudSettings{
				Current: "ops",
				Envs:    map[string]*config.CloudConfig{"prod": {}, "ops": {}},
			}},
			want: "ops",
		},
		{
			name: "sole env is used when current unset",
			cfg: config.Config{Cloud: &config.CloudSettings{
				Envs: map[string]*config.CloudConfig{"dev": {}},
			}},
			want: "dev",
		},
		{
			name: "multiple envs without current falls back to prod",
			cfg: config.Config{Cloud: &config.CloudSettings{
				Envs: map[string]*config.CloudConfig{"a": {}, "b": {}},
			}},
			want: "prod",
		},
		{
			name: "context pins an environment by name",
			cfg: config.Config{
				Cloud: &config.CloudSettings{
					Current: "prod",
					Envs:    map[string]*config.CloudConfig{"prod": {}, "dev": {}},
				},
				Contexts: map[string]*config.Context{
					"pdc-dev": {Cloud: &config.CloudConfig{Env: "dev"}},
				},
			},
			ctx:  "pdc-dev",
			want: "dev",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.cfg.ResolveCloudEnvName(tc.ctx))
		})
	}
}

func TestConfig_ResolveCloudConfig(t *testing.T) {
	base := config.Config{
		Cloud: &config.CloudSettings{
			Current: "prod",
			Envs: map[string]*config.CloudConfig{
				"prod": {Token: "env-token", APIUrl: "https://grafana.com", OAuthUrl: "https://grafana.com"},
				"dev":  {Token: "dev-token", APIUrl: "https://grafana-dev.com"},
			},
		},
		Contexts: map[string]*config.Context{},
	}

	t.Run("uses the current environment", func(t *testing.T) {
		cfg := base
		cfg.Contexts = map[string]*config.Context{"stack": {}}
		got := cfg.ResolveCloudConfig("stack")
		assert.Equal(t, "env-token", got.Token)
		assert.Equal(t, "https://grafana.com", got.APIUrl)
	})

	t.Run("per-context token overrides the environment", func(t *testing.T) {
		cfg := base
		cfg.Contexts = map[string]*config.Context{
			"stack": {Cloud: &config.CloudConfig{Token: "stack-cap", Stack: "mystack"}},
		}
		got := cfg.ResolveCloudConfig("stack")
		assert.Equal(t, "stack-cap", got.Token, "stack-scoped CAP should win")
		assert.Equal(t, "https://grafana.com", got.APIUrl, "URL still from env")
		assert.Equal(t, "mystack", got.Stack, "stack is per-context")
	})

	t.Run("context pinning an env resolves that env's auth", func(t *testing.T) {
		cfg := base
		cfg.Contexts = map[string]*config.Context{
			"stack": {Cloud: &config.CloudConfig{Env: "dev"}},
		}
		got := cfg.ResolveCloudConfig("stack")
		assert.Equal(t, "dev-token", got.Token)
		assert.Equal(t, "https://grafana-dev.com", got.APIUrl)
	})

	t.Run("no cloud section yields empty without panicking", func(t *testing.T) {
		cfg := config.Config{Contexts: map[string]*config.Context{"stack": {}}}
		got := cfg.ResolveCloudConfig("stack")
		assert.Empty(t, got.Token)
	})
}

func TestConfig_ResolveCloudAPIURL(t *testing.T) {
	t.Run("env api-url wins", func(t *testing.T) {
		cfg := config.Config{
			Cloud: &config.CloudSettings{
				Current: "ops",
				Envs:    map[string]*config.CloudConfig{"ops": {APIUrl: "https://grafana-ops.com"}},
			},
			Contexts: map[string]*config.Context{"stack": {}},
		}
		assert.Equal(t, "https://grafana-ops.com", cfg.ResolveCloudAPIURL("stack"))
	})

	t.Run("falls back to default when nothing configured", func(t *testing.T) {
		cfg := config.Config{Contexts: map[string]*config.Context{"stack": {}}}
		assert.Equal(t, "https://grafana.com", cfg.ResolveCloudAPIURL("stack"))
	})
}
