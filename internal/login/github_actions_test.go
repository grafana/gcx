package login_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/require"
)

func TestGitHubActionsLoginPersistsOnlyMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)
	result, _, err := login.RunGitHubActions(context.Background(), login.Options{Hooks: login.Hooks{ConfigSource: source, ExchangeGitHubActions: func(_ context.Context, options auth.GitHubActionsOptions) (auth.GitHubActionsResult, error) {
		require.Equal(t, "https://assistant.example", options.Endpoint)
		require.Equal(t, "1", options.TenantID)
		require.Equal(t, []string{"assistant:chat"}, options.Scopes)
		return auth.GitHubActionsResult{Token: "gat_access-secret", Tenant: "1", ExpiresAt: time.Now().Add(15 * time.Minute), APIEndpoint: options.Endpoint, GrafanaURL: "https://stack.grafana.net", Scopes: options.Scopes}, nil
	}}}, "actions", auth.GitHubActionsOptions{Endpoint: "https://assistant.example", TenantID: "1", Scopes: []string{"assistant:chat"}})
	require.NoError(t, err)
	require.Equal(t, "github-actions", result.AuthMethod)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, secret := range []string{"request-secret", "signed-oidc-secret", "gat_access-secret"} {
		require.NotContains(t, string(raw), secret)
	}
	cfg, err := config.Load(context.Background(), source)
	require.NoError(t, err)
	require.Equal(t, "actions", cfg.CurrentContext)
	require.Equal(t, "github-actions", cfg.Contexts["actions"].Grafana.AuthMethod)
	require.Equal(t, "1", cfg.Contexts["actions"].Grafana.GitHubActions.TenantID)
}
