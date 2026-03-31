package auth

import (
	"errors"
	"fmt"

	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
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
		Short: "Authenticate to a Grafana stack with OAuth",
		Long: `Opens a browser to authenticate with your Grafana stack using OAuth. This is an
alternative to using an access token.

On success, the CLI token and proxy endpoint are saved to your current config
context. Subsequent commands will use the proxy to access Grafana's API with
your identity and RBAC permissions.

Your current context must be set to a context that has a grafana server
configured before you can call this command. For example:
	gcx config set contexts.<context>.grafana.server https://your-stack.grafana.net
	gcx config use-context <context>`,
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
		return fmt.Errorf("grafana.server is not configured in context %q — set it with:\n  gcx config set contexts.<context>.grafana.server https://your-stack.grafana.net", cfg.CurrentContext)
	}

	flow := auth.NewFlow(curCtx.Grafana.Server, auth.Options{
		Writer: cmd.ErrOrStderr(),
	})
	result, err := flow.Run(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	if result.Token == "" || result.APIEndpoint == "" {
		return errors.New("authentication succeeded but the server returned incomplete token data")
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
