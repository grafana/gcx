// Package cloud provides the top-level "gcx cloud" command group for managing
// Grafana Cloud platform resources (stacks, orgs, etc.).
package cloud

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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

type gcomOAuthFlow interface {
	Run(ctx context.Context) (*auth.GCOMResult, error)
}

//nolint:gochecknoglobals // narrow test seam that proves config preflight precedes OAuth side effects.
var newGCOMOAuthFlow = func(opts auth.GCOMOptions) gcomOAuthFlow {
	return auth.NewGCOMFlow(opts)
}

func (opts *loginOpts) bindFlags(flags *pflag.FlagSet) {
	flags.StringVar(&opts.cloudToken, "cloud-token", "", "Cloud Access Policy token (skips interactive OAuth flow)")
	flags.StringVar(&opts.oauthURL, "oauth-url", "https://grafana.com", "Base URL for the OAuth login flow (used only by this command)")
	flags.StringVar(&opts.apiURL, "api-url", "https://grafana.com", "Base URL for Grafana Cloud API resource calls (stacks etc.)")
	flags.StringSliceVar(&opts.scopes, "scope", auth.DefaultGCOMScopes(), "OAuth2 scopes to request")
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
OAuth-issued token in the cloud entry's oauth-token field. Some commands that
talk to grafana.com do not yet work with an OAuth token, and the token cannot
be refreshed - when it expires, run this command again. For full
functionality, pass a Cloud Access Policy token via --cloud-token instead.

For non-interactive use (CI/CD, scripts), pass a Cloud Access Policy token
directly via --cloud-token.

The OAuth and API endpoints default to https://grafana.com. Supplying only one
of --oauth-url or --api-url selects that URL for both operations. Supplying
both preserves the explicit OAuth-origin/API-destination pair.`,
		Example: "  gcx cloud login\n  gcx cloud login --cloud-token glc_abc123",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.cloudToken = strings.TrimSpace(opts.cloudToken)
			preflightTarget, targetIsDeterministic, err := configOpts.PreflightLoginMutationTarget()
			if err != nil {
				return err
			}
			if targetIsDeterministic && preflightTarget.Type == "local" {
				return autoLocalCloudCredentialError(preflightTarget)
			}
			cfg, effectiveCtx, contextName, err := currentCloudConfig(cmd.Context(), configOpts)
			if err != nil {
				return err
			}
			mutationTarget, err := configOpts.PlanLoginMutation(cfg, contextName, config.LoginMutationCloud)
			if err != nil {
				return err
			}
			if mutationTarget.Type == "local" {
				return autoLocalCloudCredentialError(mutationTarget)
			}
			mutationSource := config.ExplicitConfigFile(mutationTarget.Path)
			mutationCtx := configOpts.LoginMutationContext(cmd.Context(), mutationTarget)
			persistedConfig, cur, err := loadPersistedCloudConfig(mutationCtx, mutationSource, contextName)
			if err != nil {
				return err
			}
			if len(cfg.Sources) > 1 {
				if err := config.VerifyLoginMutationBindings(
					mutationTarget,
					contextName,
					effectiveCtx,
					cur,
					config.LoginMutationCloud,
				); err != nil {
					return err
				}
			}
			oauthSelected := cmd.Flags().Changed("oauth-url")
			apiSelected := cmd.Flags().Changed("api-url")
			// With no endpoint flags, endpoint environment variables are explicit
			// runtime intent. Read them directly so new contexts do not lose them
			// when config env parsing initially targets another context.
			if !oauthSelected && !apiSelected {
				if envOAuth := strings.TrimSpace(os.Getenv("GRAFANA_CLOUD_OAUTH_URL")); envOAuth != "" {
					opts.oauthURL = envOAuth
					oauthSelected = true
				}
				if envAPI := strings.TrimSpace(os.Getenv("GRAFANA_CLOUD_API_URL")); envAPI != "" {
					opts.apiURL = envAPI
					apiSelected = true
				}
			}
			if err := selectCloudLoginEndpoints(opts, cur, oauthSelected, apiSelected); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			cloudSafety, err := cfg.LoginCloudMutationSafety(contextName, mutationTarget)
			if err != nil {
				return err
			}
			mutationGuard := persistedConfig.NewLoginMutationGuard(contextName, config.LoginMutationCloud)
			if mutationTarget.Type != "explicit" {
				mutationGuard, err = mutationGuard.WithDiscoverySnapshot(&cfg)
				if err != nil {
					return err
				}
			}
			if opts.cloudToken != "" {
				return runTokenLogin(mutationCtx, opts, mutationSource, contextName, cloudSafety, mutationGuard)
			}
			return runOAuthLogin(mutationCtx, opts, mutationSource, contextName, cloudSafety, mutationGuard)
		},
	}

	configOpts.BindFlags(cmd.Flags())
	opts.bindFlags(cmd.Flags())

	return cmd
}

func autoLocalCloudCredentialError(target config.ConfigSource) error {
	return gcxerrors.DetailedError{
		Summary: "Refusing fresh credentials for an auto-discovered repository config",
		Details: fmt.Sprintf(
			"The repository config %s was discovered automatically. Its Cloud endpoints and routing are repository-controlled, so gcx will not start OAuth or save a fresh Cloud token until you explicitly trust that file.",
			target.Path,
		),
		Suggestions: []string{
			"Review the file, then rerun with --config " + target.Path,
			fmt.Sprintf("Or set %s=%s after reviewing the file", config.ConfigFileEnvVar, target.Path),
		},
	}
}

// currentCloudConfig loads the effective config and returns the target context
// name used for owner-aware mutation planning. A missing context is allowed for
// first login, but malformed or unsupported configuration must fail before an
// OAuth listener or browser is started.
func currentCloudConfig(ctx context.Context, configOpts *cmdconfig.Options) (config.Config, *config.Context, string, error) {
	cfg, err := configOpts.LoadConfigTolerant(ctx)
	if errors.Is(err, os.ErrNotExist) {
		name := config.ResolveContextName(configOpts.Context, cfg)
		return cfg, nil, name, nil
	}
	if err != nil {
		return config.Config{}, nil, "", err
	}
	name := config.ResolveContextName(configOpts.Context, cfg)
	return cfg, cfg.Contexts[name], name, nil
}

// loadPersistedCloudContext reloads only the selected owner. Endpoint and
// credential decisions must not use a resolved view assembled from another
// layer after mutation planning has chosen a raw destination.
func loadPersistedCloudConfig(ctx context.Context, source config.Source, contextName string) (config.Config, *config.Context, error) {
	cfg, err := config.Load(ctx, source)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil, nil // A missing explicit file is a valid first login target.
	}
	if err != nil {
		return config.Config{}, nil, err
	}
	cfg.ResolveContext(contextName)
	return cfg, cfg.Contexts[contextName], nil
}

func runTokenLogin(
	ctx context.Context,
	opts *loginOpts,
	source config.Source,
	contextName string,
	cloudSafety config.CloudMutationSafety,
	mutationGuard config.LoginMutationGuard,
) error {
	oauthURL, apiURL := resolveCloudLoginEndpoints(opts.oauthURL, opts.apiURL)
	entry := &config.CloudEntry{
		Token:    opts.cloudToken,
		OAuthUrl: oauthURL,
		APIUrl:   apiURL,
	}
	contextName, entryName, err := config.SaveCloudConfigGuarded(ctx, source, contextName, entry, cloudSafety, mutationGuard)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Token saved to cloud entry %q (context %q)\n", entryName, contextName)
	fmt.Fprintln(os.Stderr, "Cloud token saved.")
	return nil
}

func runOAuthLogin(
	ctx context.Context,
	opts *loginOpts,
	source config.Source,
	contextName string,
	cloudSafety config.CloudMutationSafety,
	mutationGuard config.LoginMutationGuard,
) error {
	fmt.Fprintln(os.Stderr, "Warning: interactive OAuth login is experimental. It stores an OAuth-issued token in the cloud entry's oauth-token field.")
	fmt.Fprintln(os.Stderr, "Some commands that talk to grafana.com do not yet work with an OAuth token. For full functionality, use --cloud-token with a Cloud Access Policy token.")

	flow := newGCOMOAuthFlow(auth.GCOMOptions{
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

	entry := cloudEntryFromOAuthResult(opts, result)
	contextName, entryName, err := config.SaveCloudConfigGuarded(ctx, source, contextName, entry, cloudSafety, mutationGuard)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Token saved to cloud entry %q (context %q)\n", entryName, contextName)
	return nil
}

func cloudEntryFromOAuthResult(opts *loginOpts, result *auth.GCOMResult) *config.CloudEntry {
	oauthURL, apiURL := resolveCloudLoginEndpoints(opts.oauthURL, opts.apiURL)
	return &config.CloudEntry{
		OAuthToken:          result.AccessToken,
		OAuthTokenExpiresAt: result.ExpiresAt,
		OAuthScopes:         strings.Fields(result.Scope),
		OAuthUrl:            oauthURL,
		APIUrl:              apiURL,
	}
}

func resolveCloudLoginEndpoints(oauthURL, apiURL string) (string, string) {
	switch {
	case oauthURL == "" && apiURL == "":
		oauthURL, apiURL = "https://grafana.com", "https://grafana.com"
	case oauthURL == "":
		oauthURL = apiURL
	case apiURL == "":
		apiURL = oauthURL
	}
	return config.NormalizeCloudURL(oauthURL), config.NormalizeCloudURL(apiURL)
}

func selectCloudLoginEndpoints(opts *loginOpts, cur *config.Context, oauthSelected, apiSelected bool) error {
	switch {
	case oauthSelected && apiSelected:
		// Both endpoints are deliberate. Preserve the exact normalized pair.
	case oauthSelected && !apiSelected:
		opts.apiURL = opts.oauthURL
	case apiSelected && !oauthSelected:
		opts.oauthURL = opts.apiURL
	case !oauthSelected && !apiSelected:
		// Endpoint URLs are sticky across re-auth. Materialize a legacy partial
		// entry as a complete pair instead of combining it with flag defaults.
		if cur != nil && cur.CloudEntry != nil {
			opts.oauthURL = cur.CloudEntry.OAuthUrl
			opts.apiURL = cur.CloudEntry.APIUrl
		}
	}
	opts.oauthURL, opts.apiURL = resolveCloudLoginEndpoints(opts.oauthURL, opts.apiURL)
	return nil
}
