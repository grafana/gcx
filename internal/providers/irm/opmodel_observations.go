package irm

import (
	"context"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type obsCreateOpts struct {
	Context   observationContextOpts
	IO        cmdio.Options
	Container string
	Text      string
	SourceRef string
	Actor     opModelActorOpts
}

func (o *obsCreateOpts) setup(flags *pflag.FlagSet) {
	o.Context.bind(flags)
	flags.StringVar(&o.Text, "text", "", "What was noticed (required)")
	flags.StringVar(&o.SourceRef, "source-ref", "", "Where it came from; a Slack link, alert, dashboard URL (optional; empty means no source recorded)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *obsCreateOpts) Validate() error {
	if err := o.Context.validate(); err != nil {
		return err
	}
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("text", o.Text); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newObservationsCreateCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &obsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create-observation <container>",
		Short: "Record something noticed (no lifecycle; starts unverified).",
		Long: `Record an observation: raw material with no lifecycle. It carries a
fact-axis verification ("is this true"), starting unverified.`,
		Example: `  gcx irm opmodel containers create-observation INC-123 \
    --text "p99 latency doubled at 14:02" --source-ref "https://…/panel" \
    --actor-kind agent --actor-id claude-code`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runObservationsCreate(cmd, opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runObservationsCreate(cmd *cobra.Command, opts *obsCreateOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelInputs(shared, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		content := opts.Context.content(opts.Text, opts.SourceRef)
		resp, err := api.CreateObservation(ctx, createObservationRequest{
			ObservedStart: content.ObservedStart, ObservedEnd: content.ObservedEnd, Scope: content.Scope, OriginalAuthor: content.OriginalAuthor,
			Container: opts.Container,
			Actor:     opts.Actor.actor(),
			Text:      opts.Text,
			SourceRef: opts.SourceRef,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("create-observation", opts.Container,
			opModelTarget{Kind: "observation", ID: resp.ObservationID},
			nil, resp.Revision), nil
	})
}

type obsPromoteOpts struct {
	IO        cmdio.Options
	Container string
	To        string
	Text      string
	Actor     opModelActorOpts
}

func (o *obsPromoteOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.To, "to", "", "Claim type to promote into: impact or hypothesis (required)")
	flags.StringVar(&o.Text, "text", "", "Claim wording (optional; empty reuses the observation's text)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *obsPromoteOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("to", o.To); err != nil {
		return err
	}
	if err := validateEnumFlag("to", o.To, claimTypeVocab); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newObservationsPromoteCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &obsPromoteOpts{}
	cmd := &cobra.Command{
		Use:   "promote-observation <container> <observation-id>",
		Short: "Derive a claim with a versioned observation origin.",
		Long: `Derive a claim from the observation's current content version at write time.
Promotion records historical origin only. It does not consume the observation,
create evidence, or transfer verification. Attach evidence separately if intended.`,
		Example: `  gcx irm opmodel containers promote-observation INC-123 obs-42 \
    --to hypothesis \
    --actor-kind human --actor-id deric`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runObservationsPromote(cmd, args[1], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runObservationsPromote(cmd *cobra.Command, observationID string, opts *obsPromoteOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "observation-id", observationID, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.Promote(ctx, promoteRequest{
			Container:     opts.Container,
			Actor:         opts.Actor.actor(),
			ObservationID: observationID,
			Type:          opts.To,
			Text:          opts.Text,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("promote-observation", opts.Container,
			opModelTarget{Kind: "observation", ID: observationID},
			map[string]string{
				"claim_id": resp.ClaimID,
			}, resp.Revision), nil
	})
}

type obsPromoteToActionOpts struct {
	IO        cmdio.Options
	Container string
	Text      string
	Actor     opModelActorOpts
}

func (o *obsPromoteToActionOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Text, "text", "", "Action wording (optional; empty reuses the observation's text)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *obsPromoteToActionOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newObservationsPromoteToActionCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &obsPromoteToActionOpts{}
	cmd := &cobra.Command{
		Use:   "promote-observation-to-action <container> <observation-id>",
		Short: "Derive an action with a versioned observation origin.",
		Long: `Derive an action from the observation's current content version at write time.
No evidence or verification is transferred and the observation remains unchanged.`,
		Example: `  gcx irm opmodel containers promote-observation-to-action INC-123 obs-42 \
    --text "roll back the 14:00 deploy" --actor-kind human --actor-id deric`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runObservationsPromoteToAction(cmd, args[1], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runObservationsPromoteToAction(cmd *cobra.Command, observationID string, opts *obsPromoteToActionOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "observation-id", observationID, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.PromoteToAction(ctx, promoteToActionRequest{
			Container:     opts.Container,
			Actor:         opts.Actor.actor(),
			ObservationID: observationID,
			Text:          opts.Text,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("promote-observation-to-action", opts.Container,
			opModelTarget{Kind: "observation", ID: observationID},
			map[string]string{
				"action_id": resp.ActionID,
				"status":    resp.Status,
			}, resp.Revision), nil
	})
}

type obsVerifyOpts struct {
	Reason       string
	IO           cmdio.Options
	Container    string
	Verification string
	Actor        opModelActorOpts
}

func (o *obsVerifyOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Reason, "reason", "", "Reason for this judgment (optional)")
	flags.StringVar(&o.Verification, "verification", "", "Fact-axis verification: unverified, human_verified, agent_corroborated, or disputed (required). Only agent_corroborated may be set by an agent")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *obsVerifyOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("verification", o.Verification); err != nil {
		return err
	}
	if err := validateEnumFlag("verification", o.Verification, observationVerificationVocab); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newObservationsVerifyCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &obsVerifyOpts{}
	cmd := &cobra.Command{
		Use:   "verify-observation <container> <observation-id>",
		Short: "Set the fact-axis verification on an observation.",
		Long: `Set the fact-axis verification ("is this true") on an observation.
human_verified and disputed require a human actor; agent_corroborated is the
weaker, agent-producible signal.`,
		Example: `  gcx irm opmodel containers verify-observation INC-123 obs-42 \
    --verification human_verified --actor-kind human --actor-id deric`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runObservationsVerify(cmd, args[1], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runObservationsVerify(cmd *cobra.Command, observationID string, opts *obsVerifyOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "observation-id", observationID, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.VerifyObservation(ctx, verifyObservationRequest{
			Reason:        opts.Reason,
			Container:     opts.Container,
			Actor:         opts.Actor.actor(),
			ObservationID: observationID,
			Verification:  opts.Verification,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("verify-observation", opts.Container,
			opModelTarget{Kind: "observation", ID: observationID},
			map[string]string{"verification": opts.Verification},
			resp.Revision), nil
	})
}
