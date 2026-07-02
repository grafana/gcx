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
)

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
	var (
		oauthURL   string
		apiURL     string
		scopes     []string
		cloudToken string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the Grafana Cloud API (GCOM)",
		Long: `Authenticate with the Grafana Cloud API and store the token in the gcx config.

This is different from "gcx login", which authenticates to a specific
Grafana stack instance. "gcx cloud login" authenticates against the
Grafana Cloud platform API (grafana.com), enabling commands that manage
Cloud resources like stacks and access policies.

By default, opens a browser for interactive OAuth2 authentication.

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
				oauthURL = cur.Cloud.OAuthUrl
			}
			if !cmd.Flags().Changed("api-url") && cur != nil && cur.Cloud != nil && cur.Cloud.APIUrl != "" {
				apiURL = cur.Cloud.APIUrl
			}
			// Normalize whichever values won (flag, carry-over, or default)
			// so a bare host (e.g. "grafana.example.com") gets an https:// scheme
			// before it reaches the OAuth flow or is saved to config.
			oauthURL = config.NormalizeCloudURL(oauthURL)
			apiURL = config.NormalizeCloudURL(apiURL)
			if cloudToken != "" {
				return runTokenLogin(cmd.Context(), configOpts, cloudToken, oauthURL, apiURL)
			}
			return runOAuthLogin(cmd.Context(), configOpts, oauthURL, apiURL, scopes)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	cmd.Flags().StringVar(&cloudToken, "cloud-token", "", "Cloud Access Policy token (skips interactive OAuth flow)")
	cmd.Flags().StringVar(&oauthURL, "oauth-url", "https://grafana.com", "Base URL for the OAuth login flow (used only by this command)")
	cmd.Flags().StringVar(&apiURL, "api-url", "https://grafana.com", "Base URL for Grafana Cloud API resource calls (stacks etc.)")
	cmd.Flags().StringSliceVar(&scopes, "scope", []string{
		"stacks:read", "stacks:write", "stacks:delete",
		"accesspolicies:read", "accesspolicies:write", "accesspolicies:delete",
	}, "OAuth2 scopes to request")

	return cmd
}

// currentCloudContext loads the config and returns the current context, or nil
// if config can't be loaded or the context doesn't exist yet.
func currentCloudContext(ctx context.Context, configOpts *cmdconfig.Options) *config.Context {
	cfg, err := config.Load(ctx, configOpts.ConfigSource())
	if err != nil {
		return nil
	}
	ctxName := cfg.CurrentContext
	if ctxName == "" {
		ctxName = config.DefaultContextName
	}
	return cfg.Contexts[ctxName]
}

func runTokenLogin(ctx context.Context, configOpts *cmdconfig.Options, token, oauthURL, apiURL string) error {
	cloud := &config.CloudConfig{
		Token:    token,
		OAuthUrl: oauthURL,
		APIUrl:   apiURL,
	}
	if err := saveCloudConfig(ctx, configOpts, cloud); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Cloud token saved.")
	return nil
}

func runOAuthLogin(ctx context.Context, configOpts *cmdconfig.Options, oauthURL, apiURL string, scopes []string) error {
	flow := auth.NewGCOMFlow(auth.GCOMOptions{
		ClientID: defaultClientID,
		GCOMURL:  oauthURL,
		Scopes:   scopes,
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
		OAuthUrl: oauthURL,
		APIUrl:   apiURL,
	}
	return saveCloudConfig(ctx, configOpts, cloud)
}

func saveCloudConfig(ctx context.Context, configOpts *cmdconfig.Options, cloud *config.CloudConfig) error {
	source := configOpts.ConfigSource()

	cfg, err := config.Load(ctx, source)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return &gcxerrors.DetailedError{
			Summary: "Failed to load config",
			Parent:  err,
			Suggestions: []string{
				"Check your config file syntax: gcx config edit",
				"Or reset with: rm ~/.config/gcx/config.yaml && gcx cloud login",
			},
		}
	}
	if errors.Is(err, os.ErrNotExist) {
		cfg = config.Config{}
	}

	contextName := cfg.CurrentContext
	if contextName == "" {
		contextName = config.DefaultContextName
	}

	if !cfg.HasContext(contextName) {
		cfg.SetContext(contextName, true, config.Context{})
	}
	curCtx := cfg.Contexts[contextName]
	// Replace the entire cloud config to clear any stale OAuth/token fields
	// when switching from OAuth to SA token or vice versa, but preserve the
	// non-auth Stack selection so re-authenticating doesn't drop it.
	if curCtx.Cloud != nil {
		cloud.Stack = curCtx.Cloud.Stack
	}
	curCtx.Cloud = cloud

	if err := config.Write(ctx, source, cfg); err != nil {
		return &gcxerrors.DetailedError{
			Summary: "Failed to save config",
			Parent:  err,
			Suggestions: []string{
				"Check file permissions on the config file",
				"Try: gcx config edit",
			},
		}
	}

	fmt.Fprintf(os.Stderr, "Token saved to context %q\n", contextName)
	return nil
}
