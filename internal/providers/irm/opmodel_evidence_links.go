// opmodel_evidence_links.go; `gcx irm opmodel evidence-links` leaves: attach (link a
// source to a target claim with a stance), verify (inference-axis
// verification), withdraw (terminal mark; nothing is deleted).
package irm

import (
	"context"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newOpModelEvidenceLinksCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evidence-links",
		Short: "Attach, verify, and withdraw evidence relationships.",
	}
	cmd.AddCommand(
		newEvidenceLinksAttachCmd(loader, shared),
		newEvidenceLinksVerifyCmd(loader, shared),
		newEvidenceLinksWithdrawCmd(loader, shared),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// evidence-links attach
// ---------------------------------------------------------------------------

type evidenceLinksAttachOpts struct {
	IO        cmdio.Options
	Container string
	Source    string
	Target    string
	Stance    string
	Rationale string
	Actor     opModelActorOpts
}

func (o *evidenceLinksAttachOpts) setup(flags *pflag.FlagSet) {
	bindOpModelContainerFlag(flags, &o.Container)
	flags.StringVar(&o.Source, "source", "", "Source observation ID (required; claims and actions cannot be sources)")
	flags.StringVar(&o.Target, "target", "", "Target claim id; an impact or a hypothesis (required)")
	flags.StringVar(&o.Stance, "stance", "", "What the source means for the target: supports, challenges, or context (required)")
	flags.StringVar(&o.Rationale, "rationale", "", "Why the source bears on the target (optional)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *evidenceLinksAttachOpts) Validate() error {
	if err := requireOpModelFlag("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("source", o.Source); err != nil {
		return err
	}
	if err := requireOpModelFlag("target", o.Target); err != nil {
		return err
	}
	if o.Source == o.Target {
		exit2 := fail.ExitUsageError
		return &fail.DetailedError{
			Summary:     "an evidence link cannot attach an entity to itself",
			Details:     "--source and --target carried the same id: " + o.Source,
			ExitCode:    &exit2,
			Suggestions: []string{"Point --target at the claim the source bears on"},
		}
	}
	if err := requireOpModelFlag("stance", o.Stance); err != nil {
		return err
	}
	if err := validateEnumFlag("stance", o.Stance, stanceVocab); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newEvidenceLinksAttachCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &evidenceLinksAttachOpts{}
	cmd := &cobra.Command{
		Use:   "attach",
		Short: "Attach an observation interpretation to a target claim.",
		Long: `Attach an immutable evidence interpretation from an observation to an
impact or hypothesis at their current content versions at write time. It starts unverified.
Supporting evidence does not accept the claim. Replace an interpretation by
withdrawing it and attaching a new identity.`,
		Example: `  gcx irm opmodel evidence-links attach --container INC-123 \
    --source obs-42 --target clm-7 --stance supports \
    --rationale "spike aligns with the deploy" --actor-kind agent --actor-id claude-code`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runEvidenceLinksAttach(cmd, opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runEvidenceLinksAttach(cmd *cobra.Command, opts *evidenceLinksAttachOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelInputs(shared, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.AttachEvidence(ctx, attachEvidenceRequest{
			Container: opts.Container,
			Actor:     opts.Actor.actor(),
			SourceID:  opts.Source,
			TargetID:  opts.Target,
			Stance:    opts.Stance,
			Rationale: opts.Rationale,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("attach", opts.Container,
			opModelTarget{Kind: "evidence_link", ID: resp.EvidenceID},
			map[string]string{"source_id": opts.Source, "target_id": opts.Target, "stance": opts.Stance},
			resp.Revision), nil
	})
}

// ---------------------------------------------------------------------------
// evidence-links verify
// ---------------------------------------------------------------------------

type evidenceLinksVerifyOpts struct {
	IO           cmdio.Options
	Container    string
	Verification string
	Reason       string
	Actor        opModelActorOpts
}

func (o *evidenceLinksVerifyOpts) setup(flags *pflag.FlagSet) {
	bindOpModelContainerFlag(flags, &o.Container)
	flags.StringVar(&o.Verification, "verification", "", "Inference-axis verification: unverified, human_verified, or disputed (required). The server requires a human actor. A evidence link cannot be agent_corroborated; only a fact can be")
	flags.StringVar(&o.Reason, "reason", "", "Why the verification was set (optional)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *evidenceLinksVerifyOpts) Validate() error {
	if err := requireOpModelFlag("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("verification", o.Verification); err != nil {
		return err
	}
	if err := validateEnumFlag("verification", o.Verification, evidenceVerificationVocab); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newEvidenceLinksVerifyCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &evidenceLinksVerifyOpts{}
	cmd := &cobra.Command{
		Use:   "verify <evidence-id>",
		Short: "Set the inference-axis verification on an evidence link (human only).",
		Long: `Set the inference-axis verification on an evidence link: does the source mean
what the attacher says it means? Human only; the server rejects agent
actors. To detach an evidence link, use withdraw instead.`,
		Example: `  gcx irm opmodel evidence-links verify brg-5 --container INC-123 \
    --verification human_verified --actor-kind human --actor-id deric`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runEvidenceLinksVerify(cmd, args[0], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runEvidenceLinksVerify(cmd *cobra.Command, evidenceID string, opts *evidenceLinksVerifyOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "evidence-id", evidenceID, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.SetEvidenceVerification(ctx, setEvidenceVerificationRequest{
			Container:    opts.Container,
			Actor:        opts.Actor.actor(),
			EvidenceID:   evidenceID,
			Verification: opts.Verification,
			Reason:       opts.Reason,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("verify", opts.Container,
			opModelTarget{Kind: "evidence_link", ID: evidenceID},
			map[string]string{"verification": opts.Verification},
			resp.Revision), nil
	})
}

// ---------------------------------------------------------------------------
// evidence-links withdraw
// ---------------------------------------------------------------------------

type evidenceLinksWithdrawOpts struct {
	IO        cmdio.Options
	Container string
	Reason    string
	Actor     opModelActorOpts
}

func (o *evidenceLinksWithdrawOpts) setup(flags *pflag.FlagSet) {
	bindOpModelContainerFlag(flags, &o.Container)
	flags.StringVar(&o.Reason, "reason", "", "Why the evidence link was withdrawn (optional)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *evidenceLinksWithdrawOpts) Validate() error {
	if err := requireOpModelFlag("container", o.Container); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newEvidenceLinksWithdrawCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &evidenceLinksWithdrawOpts{}
	cmd := &cobra.Command{
		Use:   "withdraw <evidence-id>",
		Short: "Withdraw an evidence link without erasing its verification.",
		Long: `Withdraw an evidence interpretation without erasing its verification.
Withdrawal records a separate judgment and prevents further verification.
An agent may withdraw only its own evidence; a human may withdraw any evidence.
No version precondition or confirmation gate is required.`,
		Example: `  gcx irm opmodel evidence-links withdraw brg-5 --container INC-123 \
    --reason "misread the dashboard" --actor-kind human --actor-id deric`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runEvidenceLinksWithdraw(cmd, args[0], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runEvidenceLinksWithdraw(cmd *cobra.Command, evidenceID string, opts *evidenceLinksWithdrawOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "evidence-id", evidenceID, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.WithdrawEvidence(ctx, withdrawEvidenceRequest{
			Container:  opts.Container,
			Actor:      opts.Actor.actor(),
			EvidenceID: evidenceID,
			Reason:     opts.Reason,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("withdraw", opts.Container,
			opModelTarget{Kind: "evidence_link", ID: evidenceID},
			nil, resp.Revision), nil
	})
}
