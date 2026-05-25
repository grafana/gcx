package login

import (
	"context"
	"fmt"

	configcmd "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// tokenOpts holds inputs for `gcx login token` and its subcommands.
type tokenOpts struct {
	Config configcmd.Options
}

func (opts *tokenOpts) setup(flags *pflag.FlagSet) {
	opts.Config.BindFlags(flags)
}

func (opts *tokenOpts) Validate() error { return nil }

// tokenCommand returns the `gcx login token` Cobra command tree.
func tokenCommand() *cobra.Command {
	opts := &tokenOpts{}

	cmd := &cobra.Command{
		Use:   "token",
		Args:  cobra.NoArgs,
		Short: "Print the active Grafana token to stdout",
		Long: `Print the Grafana API token for the current (or specified) context to stdout.

By default the current context is used. Pass --context to target a different one.`,
		Example: `  gcx login token
  gcx login token --context prod`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			token, err := resolveActiveToken(cmd.Context(), opts)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), token)
			return nil
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}

// resolveActiveToken returns the best non-empty Grafana token for the
// chosen context. Preference order: API token > OAuth bearer.
func resolveActiveToken(ctx context.Context, opts *tokenOpts) (string, error) {
	cfg, err := opts.Config.LoadConfigTolerant(ctx)
	if err != nil {
		return "", fmt.Errorf("loading config: %w", err)
	}

	name := opts.Config.Context
	if name == "" {
		name = cfg.CurrentContext
	}
	if name == "" {
		return "", notLoggedInError()
	}

	c, ok := cfg.Contexts[name]
	if !ok || c == nil {
		return "", fmt.Errorf("context %q not found", name)
	}
	if c.Grafana == nil {
		return "", fmt.Errorf("context %q has no Grafana credentials - run `gcx login` first", name)
	}

	switch {
	case c.Grafana.APIToken != "":
		return c.Grafana.APIToken, nil
	case c.Grafana.OAuthToken != "":
		return c.Grafana.OAuthToken, nil
	default:
		return "", fmt.Errorf("no token stored for context %q - run `gcx login` first", name)
	}
}

func notLoggedInError() error {
	return fail.DetailedError{
		Summary: "not logged in",
		Details: "No current gcx context is configured.",
		Suggestions: []string{
			"Run `gcx login` to sign in to a Grafana instance.",
		},
	}
}
