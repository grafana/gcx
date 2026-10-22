package setup

import (
	"fmt"
	"net/mail"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/agent"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/alert"
	"github.com/grafana/gcx/internal/resources/dynamic"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// quickstartOpts holds flag values for `gcx setup quickstart`.
type quickstartOpts struct {
	IO cmdio.Options

	useDefaults bool

	contactPoint bool
	usageAlert   bool
	dashboard    bool

	// *Set fields record whether the corresponding flag was explicitly supplied
	// (via cmd.Flags().Changed). Used to distinguish "not passed" from
	// "passed as false" on bool fields, and empty-string from omitted strings.
	contactPointSet bool
	usageAlertSet   bool
	dashboardSet    bool
	emailSet        bool

	email string
}

func (o *quickstartOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("text", &quickstartTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.BoolVar(&o.useDefaults, "use-defaults", false,
		"Apply defaults without prompting (required when stdin is not a TTY)")
	flags.BoolVar(&o.contactPoint, "contact-point", false,
		"Create the email contact point. Pass --contact-point=false to skip. Omit to use default (true).")
	flags.BoolVar(&o.usageAlert, "usage-alert", false,
		"Create the usage alert rule. Pass --usage-alert=false to skip. Omit to use default (true).")
	flags.BoolVar(&o.dashboard, "dashboard", false,
		"Create the usage dashboard. Pass --dashboard=false to skip. Omit to use default (true).")
	flags.StringVar(&o.email, "email", "",
		"Email address for the contact point. Required in non-interactive mode when the contact-point or usage-alert step is enabled.")
}

func (o *quickstartOpts) Validate() error {
	if o.emailSet && strings.TrimSpace(o.email) == "" {
		return fmt.Errorf("--email must not be empty; pass an address like --email you@example.com")
	}
	if strings.TrimSpace(o.email) != "" {
		if err := validateEmail(o.email); err != nil {
			return fmt.Errorf("invalid --email %q: %w", o.email, err)
		}
	}
	return o.IO.Validate()
}

func validateEmail(value string) error {
	addr, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil || addr.Address == "" || !strings.Contains(addr.Address, "@") {
		return fmt.Errorf("expected an address like you@example.com")
	}
	return nil
}

func parsedEmail(value string) string {
	addr, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return strings.TrimSpace(value)
	}
	return addr.Address
}

func newQuickstartCommand(loader *providers.ConfigLoader) *cobra.Command {
	opts := &quickstartOpts{}
	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "Bootstrap a contact point, a usage alert, and a usage dashboard.",
		Long: `Create a small, idempotent starter set after first login: an email
contact point, a Grafana Cloud usage alert wired to it, and a dashboard of
the same active-series metric.

Re-running is safe — existing resources with the same names are left in place.
A step failure does not abort the remaining steps.

Interactive prompts require a TTY and are disabled in agent mode. Non-interactive
invocations must pass --use-defaults.`,
		Example: `  # Interactive first-time setup (TTY).
  gcx setup quickstart

  # Non-interactive with defaults.
  gcx setup quickstart --use-defaults --email you@example.com

  # Skip the dashboard.
  gcx setup quickstart --use-defaults --email you@example.com --dashboard=false

  # JSON result (agent / scripts).
  gcx setup quickstart --use-defaults --email you@example.com -o json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.contactPointSet = cmd.Flags().Changed("contact-point")
			opts.usageAlertSet = cmd.Flags().Changed("usage-alert")
			opts.dashboardSet = cmd.Flags().Changed("dashboard")
			opts.emailSet = cmd.Flags().Changed("email")

			if err := opts.Validate(); err != nil {
				return err
			}

			opts.IO.ErrWriter = cmd.ErrOrStderr()
			rn := &quickstartRunner{
				stdout:      cmd.OutOrStdout(),
				stderr:      cmd.ErrOrStderr(),
				isTTY:       promptingAllowed(term.IsTerminal(int(os.Stdin.Fd()))),
				promptYesNo: defaultPromptYesNo(cmd.InOrStdin(), cmd.ErrOrStderr()),
				promptText:  defaultPromptText(cmd.InOrStdin(), cmd.ErrOrStderr()),
			}

			plan, err := resolveQuickstartPlan(opts, rn)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			restCfg, err := loader.LoadGrafanaConfig(ctx)
			if err != nil {
				return fmt.Errorf("setup quickstart: %w", err)
			}
			alertClient, err := alert.NewClient(restCfg)
			if err != nil {
				return fmt.Errorf("setup quickstart: %w", err)
			}
			ruleClient, err := dynamic.NewDefaultNamespacedClient(restCfg)
			if err != nil {
				return fmt.Errorf("setup quickstart: %w", err)
			}
			rn.contacts = alertClient
			rn.rules = ruleClient
			return executeQuickstart(ctx, opts, plan, rn)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// promptingAllowed reports whether the interactive wizard may show prompts:
// stdin must be a real terminal AND gcx must not be running in agent mode.
func promptingAllowed(stdinIsTTY bool) bool {
	return stdinIsTTY && !agent.IsAgentMode()
}
