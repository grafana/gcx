package auth

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	internalauth "github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const unsupportedCommandsWarningTemplate = `WARNING: OAuth login is experimental. The following commands require a service account token instead:
  - frontend
  - slo
  - resources (partial)

To use a token: gcx config set %s TOKEN`

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
	Config  configcmd.Options
	Server  string
	NewFlow func(server string, opts internalauth.Options) authFlow
}

type authFlow interface {
	Run(ctx context.Context) (*internalauth.Result, error)
}

func (opts *loginOpts) setup(flags *pflag.FlagSet) {
	opts.Config.BindFlags(flags)
	flags.StringVar(&opts.Server, "server", "", "Grafana server URL to use for this login and save to the selected context")
}

func (opts *loginOpts) Validate() error {
	return nil
}

func loginCommand() *cobra.Command {
	opts := &loginOpts{
		NewFlow: func(server string, opts internalauth.Options) authFlow {
			return internalauth.NewFlow(server, opts)
		},
	}

	cmd := &cobra.Command{
		Use:   "login",
		Args:  cobra.NoArgs,
		Short: "Authenticate to a Grafana stack with OAuth (experimental)",
		Long: `Opens a browser to authenticate with your Grafana stack using OAuth. This is an
alternative to using an access token.

On success, the CLI token and proxy endpoint are saved to the selected config
context. Subsequent commands will use the proxy to access Grafana's API with
your identity and RBAC permissions.

If --server is provided, gcx uses that server for this login and saves it to
the selected context. This lets you bootstrap auth without preconfiguring
grafana.server.

Without --server, the selected context must already define grafana.server. For
example:
	gcx config set contexts.my-stack.grafana.server https://my-stack.grafana.net
	gcx config use-context my-stack

` + fmt.Sprintf(unsupportedCommandsWarningTemplate, "contexts.CONTEXT.grafana.token"),
		Example: `  gcx auth login --server https://my-stack.grafana.net
  gcx auth login --context prod --server https://prod.grafana.net
  gcx auth login`,
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

	cfg, err := loadLoginConfig(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	curCtx := cfg.GetCurrentContext()
	if curCtx == nil || curCtx.Grafana == nil || curCtx.Grafana.Server == "" {
		return fail.DetailedError{
			Summary: "Grafana server not configured",
			Details: fmt.Sprintf("Context %q does not define grafana.server.", cfg.CurrentContext),
			Suggestions: []string{
				fmt.Sprintf("Set it: gcx config set %s https://my-stack.grafana.net", formatConfigPathArg("contexts", cfg.CurrentContext, "grafana", "server")),
				"Or pass it now: gcx auth login --server https://my-stack.grafana.net",
				"Or switch context: gcx config use-context my-context",
			},
		}
	}

	flow := opts.NewFlow(curCtx.Grafana.Server, internalauth.Options{
		Writer: cmd.ErrOrStderr(),
	})
	result, err := flow.Run(ctx)
	if err != nil {
		return fmt.Errorf("authentication failed: %w", err)
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
	tokenPath := formatConfigPathArg("contexts", cfg.CurrentContext, "grafana", "token")
	fmt.Fprintf(cmd.ErrOrStderr(), "\n"+unsupportedCommandsWarningTemplate+"\n", tokenPath)

	return nil
}

func loadLoginConfig(ctx context.Context, opts *loginOpts) (config.Config, error) {
	// Auto-derive context name from server URL when no explicit --context given.
	if opts.Server != "" && opts.Config.Context == "" {
		opts.Config.Context = config.ContextNameFromServerURL(opts.Server)
	}

	var overrides []config.Override
	if opts.Server != "" && opts.Config.Context != "" {
		targetContext := opts.Config.Context
		overrides = append(overrides, func(cfg *config.Config) error {
			if cfg.HasContext(targetContext) {
				return nil
			}

			cfg.SetContext(targetContext, false, config.Context{Name: targetContext})
			return nil
		})
	}

	cfg, err := opts.Config.LoadConfigTolerant(ctx, overrides...)
	if err != nil {
		return config.Config{}, err
	}

	curCtx := cfg.GetCurrentContext()
	if curCtx == nil {
		return cfg, nil
	}

	if curCtx.Grafana == nil {
		curCtx.Grafana = &config.GrafanaConfig{}
	}
	if opts.Server != "" {
		curCtx.Grafana.Server = opts.Server
	}

	return cfg, nil
}

func formatConfigPathArg(parts ...string) string {
	escapedParts := make([]string, len(parts))
	for i, part := range parts {
		escapedParts[i] = escapeConfigPathSegment(part)
	}

	path := strings.Join(escapedParts, ".")
	if isShellSafe(path) {
		return path
	}

	return shellQuote(path)
}

func escapeConfigPathSegment(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `.`, `\.`)
}

func isShellSafe(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}

		switch r {
		case '.', '-', '_', '/', ':':
			continue
		default:
			return false
		}
	}

	return true
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
