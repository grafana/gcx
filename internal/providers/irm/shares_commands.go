// shares_commands.go; `gcx irm shares publish`: post a curated share package
// (assembled by the share-finding skill) as op model entities, then record the
// message through the independent Incident message API.
package irm

import (
	"context"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newSharesCmd(loader ShareClientLoader) *cobra.Command {
	shared := &opModelSharedOpts{}
	cmd := &cobra.Command{
		Use:    "shares",
		Hidden: true,
		Short:  "Publish curated incident findings (experimental prototype).",
		Long: `Publish curated incident findings into the operational model.

Experimental: writes to the op model prototype through the authenticated IRM
plugin resource proxy. The target and tenancy come from the active Grafana
context. Pin --context to the same context used for incident lookup and recovery.`,
	}
	shared.bind(cmd.PersistentFlags())
	cmd.AddCommand(newSharesPublishCmd(loader, shared))
	return markExperimental(cmd)
}

type sharesPublishOpts struct {
	IO             cmdio.Options
	Filename       string
	AllowUncurated bool
	AgentID        string
	AgentName      string
}

func (o *sharesPublishOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.Filename, "filename", "f", "", "Path to the share package JSON (use - for stdin) (required)")
	flags.BoolVar(&o.AllowUncurated, "allow-uncurated", false, "Testing only: publish a curated:false package. Without it, uncurated packages are refused")
	flags.StringVar(&o.AgentID, "agent-id", "claude-code", "Agent actor id recorded on the minted entities and as the message's drafted_by")
	flags.StringVar(&o.AgentName, "agent-name", "", "Agent display name recorded on the minted entities and as the message's drafted_by; defaults to --agent-id")
	// The receipt is a machine document, so JSON is the default here too; the
	// shared read helper also registers the ordered YAML codec.
	setupOpModelReadIO(&o.IO, flags)
}

func (o *sharesPublishOpts) Validate() error {
	return requireOpModelFlag("agent-id", o.AgentID)
}

func newSharesPublishCmd(loader ShareClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &sharesPublishOpts{}
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish a curated share package to the operational model.",
		Long: `Publish a version 2 share package: create its operational model entities and evidence-links,
then save its message as an Incident status update attributed to the signed-in
Grafana user. The package container must be an Incident ID for message sharing.

Requires an OAuth user login through gcx login. Service account tokens are
rejected before any entities are created. The agent is attributed on the model
entities and recorded as the message's drafted_by.

Message references are recorded in the local receipt only. They are not sent to
or persisted by the message API. A curated:false package is refused unless
--allow-uncurated is supplied for testing.

There is no rollback or idempotency. On partial failure, the receipt reports the
created IDs and the command exits 4. Re-running creates new entities. If only
message sharing failed, use gcx irm incidents share <incident-id> to share it
without creating entities again. After an ambiguous timeout, inspect the
incident first: the message may already have been saved.`,
		Example: `  gcx irm shares publish --context <my-context> -f /tmp/share-package.json --agent-id codex --agent-name Codex`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runSharesPublish(cmd, opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runSharesPublish(cmd *cobra.Command, opts *sharesPublishOpts, loader ShareClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelInputs(shared, opts); err != nil {
		return err
	}

	var pkg sharePackage
	if err := providers.ReadFileOrStdin(opts.Filename, cmd.InOrStdin(), &pkg); err != nil {
		return err
	}
	if violations := validateSharePackage(&pkg); len(violations) > 0 {
		return invalidSharePackageError(violations)
	}
	if !*pkg.Curated && !opts.AllowUncurated {
		return uncuratedSharePackageError()
	}
	if err := loader.RequireSharingOAuth(cmd.Context()); err != nil {
		return err
	}

	messages, err := loader.LoadMessages(cmd.Context(), shared.Timeout)
	if err != nil {
		return err
	}
	return withOpModel(cmd, loader, shared, func(ctx context.Context, api OpModelAPI) error {
		receipt := publishShare(ctx, api, messages, &pkg, opts.AgentID, opts.AgentName)
		if err := opts.IO.Encode(cmd.OutOrStdout(), receipt); err != nil {
			return err
		}
		if receipt.Summary.Failed > 0 {
			total := receipt.Summary.Succeeded + receipt.Summary.Failed + receipt.Summary.Skipped
			return fail.NewEmittedError(fail.ExitPartialFailure,
				fail.NewPartialFailureError("publish", total, receipt.Summary.Failed))
		}
		cmdio.EmitHint(cmd.ErrOrStderr(), "Inspect the published entities",
			"gcx irm opmodel containers get "+pkg.Container)
		return nil
	})
}
