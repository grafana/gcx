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
