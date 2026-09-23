// This test exercises private per-layer trust metadata.
//
//nolint:testpackage
package config

import (
	"testing"

	"github.com/grafana/gcx/internal/auth"
	"github.com/stretchr/testify/require"
)

func TestGitHubActionsRefusesAutoLocalConfiguration(t *testing.T) {
	grafana := &GrafanaConfig{AuthMethod: "github-actions", Server: "https://stack.grafana.net", ProxyEndpoint: "https://assistant.example", GitHubActions: &auth.GitHubActionsOptions{Endpoint: "https://assistant.example", TenantID: "1", Scopes: []string{"assistant:chat"}}}
	for _, source := range []string{"local", "explicit", "global"} {
		t.Run(source, func(t *testing.T) {
			c := &Context{Grafana: grafana, StackEntry: &StackConfig{Grafana: grafana, sourceLayer: source}}
			selection, err := c.selectGrafanaAuth()
			if source == "local" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, grafanaAuthGitHubActions, selection.mode)
		})
	}
}

func TestGitHubActionsValidationGivesConfigPath(t *testing.T) {
	for _, tt := range []struct {
		name    string
		actions *auth.GitHubActionsOptions
		path    string
	}{
		{name: "missing config", path: "github-actions"},
		{name: "endpoint mismatch", actions: &auth.GitHubActionsOptions{Endpoint: "https://other.example"}, path: "proxy-endpoint"},
		{name: "invalid options", actions: &auth.GitHubActionsOptions{Endpoint: "https://assistant.example"}, path: "github-actions"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			grafana := GrafanaConfig{AuthMethod: "github-actions", ProxyEndpoint: "https://assistant.example", GitHubActions: tt.actions}
			err := grafana.validateSelectedAuth(grafanaAuthSelection{mode: grafanaAuthGitHubActions}, "test")
			var validation ValidationError
			require.ErrorAs(t, err, &validation)
			require.Equal(t, "$.stacks.'test'.grafana."+tt.path, validation.Path)
			require.NotEmpty(t, validation.Suggestions)
		})
	}
}
