package login

import (
	"testing"

	"github.com/grafana/gcx/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeGrafanaAuthMaterializesDanglingReferencedStack(t *testing.T) {
	cfg := config.Config{
		Version: config.ConfigVersion,
		Contexts: map[string]*config.Context{
			"repair": {Name: "repair", Stack: "missing"},
		},
		CurrentContext: "repair",
	}
	cfg.Resolve()
	existing := cfg.Contexts["repair"]
	require.Nil(t, existing.StackEntry)

	err := mergeGrafanaAuthIntoStack(&cfg, existing, &config.GrafanaConfig{
		Server:     "https://example.invalid",
		APIToken:   "fresh-token",
		AuthMethod: "token",
	}, 0, "")
	require.NoError(t, err)
	require.NotNil(t, cfg.Stacks["missing"])
	require.NotNil(t, cfg.Stacks["missing"].Grafana)
	assert.Equal(t, "fresh-token", cfg.Stacks["missing"].Grafana.APIToken)
	assert.Same(t, cfg.Stacks["missing"], existing.StackEntry)
}

func TestMergeGrafanaAuthBindsSameNamedStackWithoutReplacingSettings(t *testing.T) {
	cfg := config.Config{}
	cfg.SetStack("prod", config.StackConfig{
		Slug: "keep-slug",
		Grafana: &config.GrafanaConfig{
			Server: "https://prod.example.invalid",
			OrgID:  42,
		},
		Providers: map[string]map[string]string{
			"synth": {"sm-url": "https://sm.example.invalid"},
		},
		Resources: &config.ResourcesConfig{AssumeServerDryRun: []string{"folders.folder.grafana.app"}},
	})
	cfg.SetContext("prod", true, config.Context{})
	existing := cfg.Contexts["prod"]
	originalStack := cfg.Stacks["prod"]

	err := mergeGrafanaAuthIntoStack(&cfg, existing, &config.GrafanaConfig{
		Server:     "https://prod.example.invalid",
		APIToken:   "fresh-token",
		AuthMethod: "token",
	}, 0, "")
	require.NoError(t, err)

	assert.Equal(t, "prod", existing.Stack)
	assert.Same(t, originalStack, cfg.Stacks["prod"], "the owned same-named stack must be reused, not replaced")
	assert.Equal(t, "keep-slug", cfg.Stacks["prod"].Slug)
	assert.Equal(t, "https://sm.example.invalid", cfg.Stacks["prod"].Providers["synth"]["sm-url"])
	assert.Equal(t, []string{"folders.folder.grafana.app"}, cfg.Stacks["prod"].Resources.AssumeServerDryRun)
	assert.EqualValues(t, 42, cfg.Stacks["prod"].Grafana.OrgID)
	assert.Equal(t, "fresh-token", cfg.Stacks["prod"].Grafana.APIToken)
}
