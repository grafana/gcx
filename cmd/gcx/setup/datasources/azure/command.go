package azure

import (
	"strings"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/gcxerrors"
	azonboard "github.com/grafana/gcx/internal/onboard/azure"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type azureOpts struct {
	Config           cmdconfig.Options
	IO               cmdio.Options
	Subscription     []string
	AllSubscriptions bool
	Types            []string
	Role             string
	IncludeCosmos    bool
	Cleanup          bool
	DryRun           bool
	SkipHealth       bool
	SecretExpiry     int
	Concurrency      int
	Yes              bool
	Force            bool
}

func (o *azureOpts) setup(flags *pflag.FlagSet) {
	o.Config.BindFlags(flags)
	o.IO.RegisterCustomCodec("text", &onboardTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.StringSliceVar(&o.Subscription, "subscription", nil, "Subscription ID(s) to target (default: the active subscription, or interactive multi-select)")
	flags.BoolVar(&o.AllSubscriptions, "all-subscriptions", false, "Onboard every discovered subscription non-interactively (otherwise --subscription is required when several exist)")
	flags.StringSliceVar(&o.Types, "types", nil, "Restrict to datasource kinds: azure-monitor, adx, cosmos")
	flags.StringVar(&o.Role, "role", "", "Override the default Azure role set (comma-separated role names, e.g. \"Monitoring Reader\")")
	flags.BoolVar(&o.IncludeCosmos, "include-cosmos", false, "Include Azure Cosmos DB datasources (requires the Enterprise plugin licensed in Grafana)")
	flags.BoolVar(&o.Cleanup, "cleanup", false, "Remove gcx-created Azure app registrations and their datasources")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview what would be created or removed without making any changes")
	flags.BoolVar(&o.SkipHealth, "skip-health-check", false, "Skip the post-create datasource health verification")
	flags.IntVar(&o.SecretExpiry, "secret-expiry-days", 0, "Set an expiry (in days) on minted client secrets (0 = Azure default). Rotate before expiry with the rotate subcommand")
	flags.IntVar(&o.Concurrency, "concurrency", 0, "Maximum datasources to provision in parallel (0 = default; interactive runs are always serial)")
	flags.BoolVar(&o.Yes, "yes", false, "Non-interactive: skip prompts and accept all suggestions")
	flags.BoolVar(&o.Force, "force", false, "Confirm credential-minting side effects (required in agent mode); implies --yes")
}

func (o *azureOpts) Validate() error {
	if err := o.validateTypes(); err != nil {
		return err
	}

	// --dry-run never mutates, so it is always exempt from the agent-mode --force
	// gate. Both provisioning (mints credentials) and cleanup (deletes cloud
	// artifacts) mutate, so they require --force in agent mode, where gcx cannot
	// prompt for confirmation.
	if agent.IsAgentMode() && !o.Force && !o.DryRun {
		if o.Cleanup {
			return gcxerrors.DetailedError{
				Summary: "setup datasources azure --cleanup deletes cloud artifacts; --force is required in agent mode",
				Details: "Cleanup permanently removes gcx-created Azure app registrations, role assignments, and Grafana datasources.",
				Suggestions: []string{
					"Re-run with --cleanup --force",
					"Or preview first with --cleanup --dry-run (nothing is deleted)",
				},
			}
		}
		return gcxerrors.DetailedError{
			Summary: "setup datasources azure mints cloud credentials; --force is required in agent mode",
			Details: "Provisioning creates Azure app registrations, role assignments, and Grafana datasources.",
			Suggestions: []string{
				"Re-run with --force [--subscription <id>] [--types azure-monitor]",
				"Or preview first with --dry-run (no credentials are minted)",
			},
		}
	}

	return o.IO.Validate()
}

// validateTypes rejects unknown --types values up front so a typo (e.g.
// "--types adxx") fails with the valid set rather than silently matching no
// suggestions and reporting "nothing was created".
func (o *azureOpts) validateTypes() error {
	if len(o.Types) == 0 {
		return nil
	}
	valid := make(map[string]bool, len(azonboard.TypeTokens()))
	for _, t := range azonboard.TypeTokens() {
		valid[t] = true
	}
	var unknown []string
	for _, t := range o.Types {
		norm := strings.ToLower(strings.TrimSpace(t))
		if norm != "" && !valid[norm] {
			unknown = append(unknown, t)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return gcxerrors.DetailedError{
		Summary: "unknown --types value(s): " + strings.Join(unknown, ", "),
		Suggestions: []string{
			"Valid types are: " + strings.Join(azonboard.TypeTokens(), ", "),
		},
	}
}

// Command returns the `gcx setup datasources azure` command.
func Command() *cobra.Command {
	opts := &azureOpts{}

	cmd := &cobra.Command{
		Use:   "azure",
		Short: "Onboard Azure datasources (Azure Monitor, Azure Data Explorer, and Azure CosmosDB)",
		Long: "Discover Azure resources using your local `az` CLI session and provision the " +
			"matching Grafana datasources. For each accepted datasource, gcx mints a dedicated, " +
			"gcx-owned Azure app registration (service principal + secret + least-privilege role) " +
			"with you set as owner, then creates the Grafana datasource wired to those credentials.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint: "Mints Azure credentials and creates datasources; requires --force in agent mode (or use --dry-run to preview without minting). " +
				"Example: gcx setup datasources azure --force --types azure-monitor --subscription <sub-id> -o json. " +
				"Clean up gcx-created artifacts with: gcx setup datasources azure --cleanup --force -o json. " +
				"Rotate minted secrets with: gcx setup datasources azure rotate --force -o json",
		},
		Example: `
  # Interactive: discover and pick datasources to create
  gcx setup datasources azure

  # Preview what would be created without minting anything
  gcx setup datasources azure --dry-run

  # Non-interactive: create the Azure Monitor datasource with a specified subscription
  gcx setup datasources azure --force --types azure-monitor --subscription <sub-id>

  # Tighten Azure Monitor permissions to metrics only
  gcx setup datasources azure --types azure-monitor --role "Monitoring Reader"

  # Remove everything gcx created
  gcx setup datasources azure --cleanup --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return runAzure(cmd, opts)
		},
	}

	opts.setup(cmd.Flags())
	cmd.AddCommand(rotateCommand())
	return cmd
}
