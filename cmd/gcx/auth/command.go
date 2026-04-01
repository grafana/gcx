package auth

import (
	"errors"
	"fmt"

	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

type loginOpts struct {
	Config configcmd.Options
}

func (opts *loginOpts) setup(flags *pflag.FlagSet) {
	opts.Config.BindFlags(flags)
}

func (opts *loginOpts) Validate() error {
	return nil
}

func loginCommand() *cobra.Command {
	opts := &loginOpts{}

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
			if err := opts.Validate(); err != nil {
				return err
			}
			return runLogin(cmd, opts)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

func runLogin(cmd *cobra.Command, opts *loginOpts) error {
	ctx := cmd.Context()

	cfg, err := opts.Config.LoadConfigTolerant(ctx)
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

	if err := auth.ValidateEndpointURL(result.APIEndpoint); err != nil {
		return fmt.Errorf("server returned untrusted API endpoint %q: %w", result.APIEndpoint, err)
	}

	// Save tokens to the current context.
	curCtx.Grafana.ProxyEndpoint = result.APIEndpoint
	curCtx.Grafana.OAuthToken = result.Token
	curCtx.Grafana.OAuthRefreshToken = result.RefreshToken
	curCtx.Grafana.OAuthTokenExpiresAt = result.ExpiresAt
	curCtx.Grafana.OAuthRefreshExpiresAt = result.RefreshExpiresAt

	if err := config.Write(ctx, opts.Config.ConfigSource(), cfg); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Authenticated as %s. Tokens saved to context %q.\n", result.Email, cfg.CurrentContext)

	return nil
}
