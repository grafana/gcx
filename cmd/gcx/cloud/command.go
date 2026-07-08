// Package cloud provides the top-level "gcx cloud" command group for managing
// Grafana Cloud platform resources (stacks, orgs, etc.).
package cloud

import (
	"context"
	"errors"
	"fmt"
	"os"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/stacks"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type loginOpts struct {
	oauthURL   string
	apiURL     string
	scopes     []string
	cloudToken string
}

func (opts *loginOpts) bindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&opts.cloudToken, "cloud-token", "", "Cloud Access Policy token (skips interactive OAuth flow)")
	flags.StringVar(&opts.oauthURL, "oauth-url", "https://grafana.com", "Base URL for the OAuth login flow (used only by this command)")
	flags.StringVar(&opts.apiURL, "api-url", "https://grafana.com", "Base URL for Grafana Cloud API resource calls (stacks etc.)")
	// The grafana.com API scopes gcx needs across all commands: stacks
	// (discovery + management), the signal write scopes for minting the
	// Synthetic Monitoring token (metrics/logs/traces:write), and Fleet
	// Management.
	flags.StringSliceVar(&opts.scopes, "scope", []string{
		"stacks:read", "stacks:write", "stacks:delete",
		"metrics:write",
		"logs:write",
		"traces:write",
		"fleet-management:read", "fleet-management:write",
	}, "OAuth2 scopes to request")
}

func (opts *loginOpts) Validate() error {
	if opts.oauthURL == "" {
		return errors.New("--oauth-url must not be empty")
	}
	if opts.apiURL == "" {
		return errors.New("--api-url must not be empty")
	}
	if opts.cloudToken == "" && len(opts.scopes) == 0 {
		return errors.New("--scope must not be empty for interactive OAuth login")
	}
	return nil
}

// Command returns the top-level "cloud" cobra command with all subcommands.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Manage your Grafana Cloud resources",
	}

	cmd.AddCommand(stacks.NewCommand())
	cmd.AddCommand(loginCmd())

	return cmd
}

const defaultClientID = "gcx"

func loginCmd() *cobra.Command {
	configOpts := &cmdconfig.Options{}
	opts := &loginOpts{}

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the Grafana Cloud API (GCOM)",
		Long: `Authenticate with the Grafana Cloud API and store the token in the gcx config.

This is different from "gcx login", which authenticates to a specific
Grafana stack instance. "gcx cloud login" authenticates against the
Grafana Cloud platform API (grafana.com), enabling commands that manage
Cloud resources like stacks and access policies.

By default, opens a browser for interactive OAuth2 authentication.

EXPERIMENTAL: interactive OAuth login is an experimental flow that stores an
OAuth-issued token as the context's cloud.token. Some commands that talk to
grafana.com do not yet work with an OAuth token. For full functionality, pass
a Cloud Access Policy token via --cloud-token instead.

For non-interactive use (CI/CD, scripts), pass a Cloud Access Policy token
directly via --cloud-token.

Two endpoints can be configured independently, both defaulting to
https://grafana.com: --oauth-url is used only for the login flow here, while
--api-url is used by every command that talks to the Grafana Cloud API.`,
		Example: `  gcx cloud login
  gcx cloud login --cloud-token glsa_abc123`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Endpoint URLs are sticky across re-auth: when not passed
			// explicitly, carry over whatever the current context already has so
			// a plain `gcx cloud login` doesn't wipe a previously set value.
			cur := currentCloudContext(cmd.Context(), configOpts)
			if !cmd.Flags().Changed("oauth-url") && cur != nil && cur.Cloud != nil && cur.Cloud.OAuthUrl != "" {
				opts.oauthURL = cur.Cloud.OAuthUrl
			}
			if !cmd.Flags().Changed("api-url") && cur != nil && cur.Cloud != nil && cur.Cloud.APIUrl != "" {
				opts.apiURL = cur.Cloud.APIUrl
			}
			// Normalize whichever values won (flag, carry-over, or default)
			// so a bare host (e.g. "grafana.example.com") gets an https:// scheme
			// before it reaches the OAuth flow or is saved to config.
			opts.oauthURL = config.NormalizeCloudURL(opts.oauthURL)
			opts.apiURL = config.NormalizeCloudURL(opts.apiURL)
			if err := opts.Validate(); err != nil {
				return err
			}
			if opts.cloudToken != "" {
				return runTokenLogin(cmd.Context(), configOpts, opts)
			}
			return runOAuthLogin(cmd.Context(), configOpts, opts)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.bindFlags(cmd.Flags())

	return cmd
}

// currentCloudContext loads the config and returns the target context, or nil
// if config can't be loaded or the context doesn't exist yet.
func currentCloudContext(ctx context.Context, configOpts *cmdconfig.Options) *config.Context {
	cfg, err := config.Load(ctx, configOpts.ConfigSource())
	if err != nil {
		return nil
	}
	return cfg.Contexts[config.ResolveContextName(configOpts.Context, cfg)]
}

func runTokenLogin(ctx context.Context, configOpts *cmdconfig.Options, opts *loginOpts) error {
	cloud := &config.CloudConfig{
		Token:    opts.cloudToken,
		OAuthUrl: opts.oauthURL,
		APIUrl:   opts.apiURL,
	}
	contextName, err := config.SaveCloudConfig(ctx, configOpts.ConfigSource(), configOpts.Context, cloud)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Token saved to context %q\n", contextName)
	fmt.Fprintln(os.Stderr, "Cloud token saved.")
	return nil
}

func runOAuthLogin(ctx context.Context, configOpts *cmdconfig.Options, opts *loginOpts) error {
	fmt.Fprintln(os.Stderr, "Warning: interactive OAuth login is experimental. It stores an OAuth-issued token as the context's cloud.token.")
	fmt.Fprintln(os.Stderr, "Some commands that talk to grafana.com do not yet work with an OAuth token. For full functionality, use --cloud-token with a Cloud Access Policy token.")

	flow := auth.NewGCOMFlow(auth.GCOMOptions{
		ClientID: defaultClientID,
		GCOMURL:  opts.oauthURL,
		Scopes:   opts.scopes,
		Writer:   os.Stderr,
	})

	result, err := flow.Run(ctx)
	if err != nil {
		return &gcxerrors.DetailedError{
			Summary: "Authentication failed",
			Parent:  err,
			Suggestions: []string{
				"Check that the OAuth login URL is correct",
				"Ensure you are logged in to Grafana Cloud in your browser",
				"Try again with: gcx cloud login --oauth-url <url>",
			},
		}
	}

	fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\n", result.Info.Login, result.Info.Email)
	fmt.Fprintf(os.Stderr, "Scopes: %s\n", result.Scope)

	cloud := &config.CloudConfig{
		Token:    result.AccessToken,
		OAuthUrl: opts.oauthURL,
		APIUrl:   opts.apiURL,
	}
	contextName, err := config.SaveCloudConfig(ctx, configOpts.ConfigSource(), configOpts.Context, cloud)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Token saved to context %q\n", contextName)
	return nil
}
