package login_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/login"
	"github.com/stretchr/testify/require"
)

type actionsTransport func(*http.Request) (*http.Response, error)

func (f actionsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubActionsLoginPersistsOnlyMetadata(t *testing.T) {
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "https://pipelines.actions.githubusercontent.com/token")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "request-secret")
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = actionsTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"value":"signed-oidc-secret"}`
		if r.URL.Host == "assistant.example" {
			require.Equal(t, "/api/cli/v1/auth/github-actions", r.URL.Path)
			data, err := json.Marshal(map[string]any{"data": auth.GitHubActionsResult{Token: "gat_access-secret", Tenant: "1", ExpiresAt: time.Now().Add(15 * time.Minute), APIEndpoint: "https://assistant.example", GrafanaURL: "https://stack.grafana.net", Scopes: []string{"assistant:chat"}}})
			require.NoError(t, err)
			body = string(data)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	path := filepath.Join(t.TempDir(), "config.yaml")
	source := config.ExplicitConfigFile(path)
	result, _, err := login.RunGitHubActions(context.Background(), login.Options{Hooks: login.Hooks{ConfigSource: source}}, "actions", auth.GitHubActionsOptions{Endpoint: "https://assistant.example", TenantID: "1", Scopes: []string{"assistant:chat"}})
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
