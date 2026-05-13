package cloud

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/auth"
	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/fail"
	"github.com/spf13/cobra"
)

func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Manage Grafana Cloud resources",
	}

	configOpts := &cmdconfig.Options{}
	configOpts.BindFlags(cmd.PersistentFlags())

	cmd.AddCommand(loginCmd(configOpts))

	return cmd
}

const defaultClientID = "gcx"

func loginCmd(configOpts *cmdconfig.Options) *cobra.Command {
	var (
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

By default, opens a browser for interactive OAuth2 authentication where
you can choose which scopes and organization to grant access to.

For non-interactive use (CI/CD, scripts), pass a Cloud Access Policy token
directly via --cloud-token.`,
		Example: `  gcx cloud login
  gcx cloud login --cloud-token glsa_abc123
  gcx cloud login --api-url https://grafana-dev.com`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cloudToken != "" {
				return runTokenLogin(cmd.Context(), configOpts, cloudToken, apiURL)
			}
			return runOAuthLogin(cmd.Context(), configOpts, apiURL, scopes)
		},
	}

	cmd.Flags().StringVar(&cloudToken, "cloud-token", "", "Cloud Access Policy token (skips interactive OAuth flow)")
	cmd.Flags().StringVar(&apiURL, "api-url", "https://grafana.com", "GCOM API base URL")
	cmd.Flags().StringSliceVar(&scopes, "scope", []string{
		"stacks:read", "stacks:write", "stacks:delete",
		"accesspolicies:read", "accesspolicies:write", "accesspolicies:delete",
	}, "OAuth2 scopes to request")

	return cmd
}

func runTokenLogin(ctx context.Context, configOpts *cmdconfig.Options, token, apiURL string) error {
	cloud := &config.CloudConfig{
		Token:  token,
		APIUrl: apiURL,
	}
	if err := saveCloudConfig(ctx, configOpts, cloud); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Cloud token saved.")
	return nil
}

func runOAuthLogin(ctx context.Context, configOpts *cmdconfig.Options, apiURL string, scopes []string) error {
	flow := auth.NewGCOMFlow(auth.GCOMOptions{
		ClientID: defaultClientID,
		GCOMURL:  apiURL,
		Scopes:   scopes,
		Writer:   os.Stderr,
	})

	result, err := flow.Run(ctx)
	if err != nil {
		return &fail.DetailedError{
			Summary: "Authentication failed",
			Parent:  err,
			Suggestions: []string{
				"Check that the GCOM API URL is correct",
				"Ensure you are logged in to Grafana Cloud in your browser",
				"Try again with: gcx cloud login --api-url <url>",
			},
		}
	}

	fmt.Fprintf(os.Stderr, "Authenticated as %s (%s)\n", result.Info.Login, result.Info.Email)
	fmt.Fprintf(os.Stderr, "Scopes: %s\n", result.Scope)
	if result.OrgSlug != "" {
		fmt.Fprintf(os.Stderr, "Organization: %s\n", result.OrgSlug)
	}

	cloud := &config.CloudConfig{
		Token:            result.AccessToken,
		TokenExpiresAt:   time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339),
		RefreshToken:     result.RefreshToken,
		RefreshExpiresAt: result.RefreshExpiresAt,
		Org:              result.OrgSlug,
		APIUrl:           apiURL,
	}
	return saveCloudConfig(ctx, configOpts, cloud)
}

func saveCloudConfig(ctx context.Context, configOpts *cmdconfig.Options, cloud *config.CloudConfig) error {
	source := configOpts.ConfigSource()
	if source == nil {
		source = config.StandardLocation()
	}

	cfg, err := config.Load(ctx, source)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return &fail.DetailedError{
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
	// replace the entire cloud config — clears any stale OAuth fields
	// when switching from OAuth to SA token or vice versa
	curCtx.Cloud = cloud

	if err := config.Write(ctx, source, cfg); err != nil {
		return &fail.DetailedError{
			Summary: "Failed to save config",
			Parent:  err,
			Suggestions: []string{
				"Check file permissions on " + contextName,
				"Try: gcx config edit",
			},
		}
	}

	fmt.Fprintf(os.Stderr, "Token saved to context %q\n", contextName)
	return nil
}
