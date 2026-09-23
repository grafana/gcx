package login

import (
	"context"
	"errors"
	"strconv"

	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
)

// RunGitHubActions verifies a delegation and persists only its non-secret settings.
// It uses the same guarded mutation and auth merge as browser login.
func RunGitHubActions(ctx context.Context, opts Options, name string, actions auth.GitHubActionsOptions) (Result, string, error) {
	stackID, err := strconv.ParseInt(actions.TenantID, 10, 64)
	if err != nil || stackID <= 0 {
		return Result{}, "", errors.New("tenant-id must be a positive Grafana stack ID")
	}
	exchange := opts.ExchangeGitHubActions
	if exchange == nil {
		exchange = exchangeGitHubActions
	}
	response, err := exchange(ctx, actions)
	if err != nil {
		return Result{}, "", err
	}
	if name == "" {
		name = "github-actions"
	}
	g := &config.GrafanaConfig{Server: response.GrafanaURL, StackID: stackID, ProxyEndpoint: actions.Endpoint, AuthMethod: "github-actions", GitHubActions: &actions}
	if err := persistContext(ctx, opts, name, config.Context{Grafana: g}, ""); err != nil {
		return Result{}, "", err
	}
	return Result{ContextName: name, AuthMethod: "github-actions", IsCloud: true}, response.GrafanaURL, nil
}

func exchangeGitHubActions(ctx context.Context, actions auth.GitHubActionsOptions) (auth.GitHubActionsResult, error) {
	client, err := auth.NewGitHubActions(actions)
	if err != nil {
		return auth.GitHubActionsResult{}, err
	}
	return client.Exchange(ctx)
}
