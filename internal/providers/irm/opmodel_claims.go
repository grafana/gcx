package irm

import (
	"context"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type claimsCreateOpts struct {
	IO        cmdio.Options
	Container string
	ClaimType string
	Text      string
	Actor     opModelActorOpts
}

func (o *claimsCreateOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.ClaimType, "type", "", "What the claim asserts: impact (what is broken, for whom) or hypothesis (why) (required)")
	flags.StringVar(&o.Text, "text", "", "The assertion itself (required)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *claimsCreateOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("type", o.ClaimType); err != nil {
		return err
	}
	if err := validateEnumFlag("type", o.ClaimType, claimTypeVocab); err != nil {
		return err
	}
	if err := requireOpModelFlag("text", o.Text); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newClaimsCreateCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &claimsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create-claim <container>",
		Short: "State an impact or hypothesis outright.",
		Long: `State an impact or hypothesis without an observation origin.
Claims start unassessed; impacts also start with unknown recovery.
Assessment and recovery are independent human judgments.`,
		Example: `  gcx irm opmodel containers create-claim INC-123 --type impact \
    --text "checkout is failing for EU users" --actor-kind agent --actor-id claude-code`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runClaimsCreate(cmd, opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runClaimsCreate(cmd *cobra.Command, opts *claimsCreateOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelInputs(shared, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.CreateClaim(ctx, createClaimRequest{
			Container: opts.Container,
			Actor:     opts.Actor.actor(),
			Type:      opts.ClaimType,
			Text:      opts.Text,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("create-claim", opts.Container,
			opModelTarget{Kind: "claim", ID: resp.ClaimID},
			nil,
			resp.Revision), nil
	})
}

type claimsAssessOpts struct {
	flags                *pflag.FlagSet
	IO                   cmdio.Options
	Container            string
	ImpactAssessment     string
	HypothesisAssessment string
	Reason               string
	Actor                opModelActorOpts
}

func (o *claimsAssessOpts) setup(flags *pflag.FlagSet) {
	o.flags = flags
	flags.StringVar(&o.ImpactAssessment, "impact-assessment", "", "Impact assessment: unassessed, confirmed, ruled_out (exactly one typed assessment required)")
	flags.StringVar(&o.HypothesisAssessment, "hypothesis-assessment", "", "Hypothesis assessment: unassessed, accepted, ruled_out (exactly one typed assessment required)")
	flags.StringVar(&o.Reason, "reason", "", "Reason for this assessment (optional)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *claimsAssessOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if o.flags.Changed("impact-assessment") == o.flags.Changed("hypothesis-assessment") {
		return opModelUsage("exactly one of --impact-assessment or --hypothesis-assessment is required")
	}
	if o.flags.Changed("impact-assessment") {
		if err := validateEnumFlag("impact-assessment", o.ImpactAssessment, impactAssessmentVocab); err != nil {
			return err
		}
	} else if err := validateEnumFlag("hypothesis-assessment", o.HypothesisAssessment, hypothesisAssessmentVocab); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newClaimsAssessCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &claimsAssessOpts{}
	cmd := &cobra.Command{
		Use:   "assess-claim <container> <claim-id>",
		Short: "Assess an impact or hypothesis (human judgment).",
		Long: `Record one typed assessment against the claim's current content at write time.
This does not change impact recovery. The server requires a human
actor; agents must not impersonate a human after prose approval.`,
		Example: `  gcx irm opmodel containers assess-claim INC-123 clm-7 \
    --impact-assessment confirmed --reason "reproduced on staging" \
    --actor-kind human --actor-id deric`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runClaimsAssess(cmd, args[1], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runClaimsAssess(cmd *cobra.Command, claimID string, opts *claimsAssessOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "claim-id", claimID, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.AssessClaim(ctx, assessClaimRequest{
			Container:        opts.Container,
			Actor:            opts.Actor.actor(),
			ClaimID:          claimID,
			ImpactAssessment: opts.ImpactAssessment, HypothesisAssessment: opts.HypothesisAssessment,
			Reason: opts.Reason,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("assess-claim", opts.Container,
			opModelTarget{Kind: "claim", ID: claimID},
			map[string]string{"impact_assessment": opts.ImpactAssessment, "hypothesis_assessment": opts.HypothesisAssessment},
			resp.Revision), nil
	})
}
