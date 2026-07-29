package azure

import (
	"io"

	cmdconfig "github.com/grafana/gcx/cmd/gcx/config"
	"github.com/grafana/gcx/internal/agent"
	dsclient "github.com/grafana/gcx/internal/datasources"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/onboard"
	azonboard "github.com/grafana/gcx/internal/onboard/azure"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/grafana-app-sdk/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type rotateOpts struct {
	Config       cmdconfig.Options
	IO           cmdio.Options
	SecretExpiry int
	SkipHealth   bool
	DryRun       bool
	Force        bool
}

func (o *rotateOpts) setup(flags *pflag.FlagSet) {
	o.Config.BindFlags(flags)
	o.IO.RegisterCustomCodec("text", &onboardTextCodec{})
	o.IO.DefaultFormat("text")
	o.IO.BindFlags(flags)

	flags.IntVar(&o.SecretExpiry, "secret-expiry-days", 0, "Set an expiry (in days) on the newly minted client secrets (0 = Azure default)")
	flags.BoolVar(&o.SkipHealth, "skip-health-check", false, "Skip the post-rotation datasource health verification")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview which datasources would have their secret rotated without changing anything")
	flags.BoolVar(&o.Force, "force", false, "Confirm credential-mutating side effects (required in agent mode)")
}

func (o *rotateOpts) Validate() error {
	if agent.IsAgentMode() && !o.Force && !o.DryRun {
		return gcxerrors.DetailedError{
			Summary: "rotate mints new cloud credentials; --force is required in agent mode",
			Details: "Rotation mints a new client secret for each gcx-managed datasource, updates the datasource, and retires the old secret.",
			Suggestions: []string{
				"Re-run with --force",
				"Or preview first with --dry-run (nothing is changed)",
			},
		}
	}
	return o.IO.Validate()
}

// rotateCommand returns the `gcx setup datasources azure rotate` subcommand.
func rotateCommand() *cobra.Command {
	opts := &rotateOpts{}
	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate client secrets for gcx-created Azure datasources",
		Long: "Mint a fresh client secret for each gcx-managed Azure datasource, update the " +
			"datasource to use it, and retire the superseded secret. Only datasources whose " +
			"backing app registration is tagged gcx-managed (and attributable to you) are rotated; " +
			"key-based datasources (Cosmos DB) are skipped.",
		Annotations: map[string]string{
			agent.AnnotationTokenCost: "small",
			agent.AnnotationLLMHint: "Rotates client secrets for gcx-created Azure datasources; requires --force in agent mode (or --dry-run to preview). " +
				"Example: gcx setup datasources azure rotate --force -o json",
		},
		Example: `
  # Preview which datasources would be rotated
  gcx setup datasources azure rotate --dry-run

  # Rotate all gcx-managed Azure datasource secrets
  gcx setup datasources azure rotate --force`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			return runRotate(cmd, opts)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runRotate(cmd *cobra.Command, opts *rotateOpts) error {
	ctx := cmd.Context()
	log := logging.FromContext(ctx)
	errOut := cmd.ErrOrStderr()

	var progress io.Writer
	if !agent.IsAgentMode() {
		progress = errOut
	}

	onboard.Progressf(progress, "Checking for the Azure CLI (az)...")
	cli := azonboard.NewCLI()
	if err := cli.Ensure(); err != nil {
		return err
	}

	onboard.Progressf(progress, "Connecting to Grafana...")
	restCfg, err := opts.Config.LoadGrafanaConfig(ctx)
	if err != nil {
		return err
	}
	ds, err := dsclient.NewClient(restCfg)
	if err != nil {
		return err
	}

	deps := azonboard.RunDeps{CLI: cli, DS: ds, Log: log, ErrOut: errOut, Progress: progress}

	// Best-effort caller identity so rotation only touches app registrations
	// attributable to this caller.
	callerOID, _ := cli.SignedInUserObjectID(ctx)

	res, err := azonboard.Rotate(ctx, deps, azonboard.RotateInput{
		CallerOID:  callerOID,
		Stack:      stackLabel(restCfg),
		ExpiryDays: opts.SecretExpiry,
		DryRun:     opts.DryRun,
		SkipHealth: opts.SkipHealth,
	})
	if err != nil {
		return err
	}
	return opts.IO.Encode(cmd.OutOrStdout(), res)
}
