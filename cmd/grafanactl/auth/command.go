package auth

import (
	"fmt"

	configcmd "github.com/grafana/grafanactl/cmd/grafanactl/config"
	"github.com/grafana/grafanactl/internal/auth"
	"github.com/grafana/grafanactl/internal/config"
	"github.com/spf13/cobra"
)

// Command returns the `auth` command group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}

	cmd.AddCommand(loginCommand())

	return cmd
}

func loginCommand() *cobra.Command {
	configOpts := &configcmd.Options{}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate to a Grafana stack via browser",
		Long: `Opens a browser to authenticate with your Grafana stack using OAuth.

On success, the CLI token and proxy endpoint are saved to your current config
context. Subsequent commands will use the proxy to access Grafana's API with
your identity and RBAC permissions.

The Grafana server URL must already be configured in the current context
(e.g., via 'grafanactl config set grafana.server https://your-stack.grafana.net').`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, configOpts)
		},
	}

	configOpts.BindFlags(cmd.Flags())

	return cmd
}

func runLogin(cmd *cobra.Command, configOpts *configcmd.Options) error {
	ctx := cmd.Context()

	cfg, err := configOpts.LoadConfigTolerant(ctx)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	curCtx := cfg.GetCurrentContext()
	if curCtx == nil || curCtx.Grafana == nil || curCtx.Grafana.Server == "" {
		return fmt.Errorf("grafana.server is not configured in context %q — set it with:\n  grafanactl config set grafana.server https://your-stack.grafana.net", cfg.CurrentContext)
	}

	flow := auth.NewFlow(curCtx.Grafana.Server, auth.Options{})
	result, err := flow.Run(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if result.Token == "" || result.APIEndpoint == "" {
		return fmt.Errorf("authentication succeeded but the server returned incomplete token data")
	}

	// Save tokens to the current context.
	curCtx.Grafana.ProxyEndpoint = result.APIEndpoint
	curCtx.Grafana.CLIToken = result.Token
	curCtx.Grafana.CLIRefreshToken = result.RefreshToken
	curCtx.Grafana.CLITokenExpiresAt = result.ExpiresAt
	curCtx.Grafana.CLIRefreshExpiresAt = result.RefreshExpiresAt

	if err := config.Write(ctx, configOpts.ConfigSource(), cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Authenticated as %s. Tokens saved to context %q.\n", result.Email, cfg.CurrentContext)

	return nil
}
