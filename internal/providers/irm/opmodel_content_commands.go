package irm

import (
	"context"
	"strings"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newObservationsEditCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &obsCreateOpts{}
	cmd := &cobra.Command{
		Use:     "update-observation <container> <observation-id>",
		Short:   "Replace observation content, clearing omitted optional fields.",
		Long:    "Replace text, source, observed interval, scope, and original author. Omitted optional values are cleared. Use update-entity-text to preserve context while changing wording.",
		Example: "  gcx irm opmodel containers update-observation INC-123 obs-1 --text 'corrected observation' --actor-kind human --actor-id responder",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := validateOpModelIDInputs(shared, "observation-id", args[1], opts); err != nil {
				return err
			}
			return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
				resp, err := api.EditObservation(ctx, editObservationRequest{Container: opts.Container, ObservationID: args[1], Actor: opts.Actor.actor(), Content: opts.Context.content(opts.Text, opts.SourceRef)})
				if err != nil {
					return opModelMutation{}, err
				}
				return newOpModelMutation("update-observation", opts.Container, opModelTarget{Kind: "observation", ID: args[1]}, nil, resp.Revision), nil
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type actionsEditOpts struct {
	IO        cmdio.Options
	Container string
	Text      string
	Content   actionContentOpts
	Actor     opModelActorOpts
}

func (o *actionsEditOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Text, "text", "", "Replacement action wording (required)")
	o.Content.bind(flags)
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}
func (o *actionsEditOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("text", o.Text); err != nil {
		return err
	}
	return o.Actor.validate()
}
func newActionsEditCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &actionsEditOpts{}
	cmd := &cobra.Command{
		Use:     "update-action <container> <action-id>",
		Short:   "Replace action content, clearing an omitted expected result.",
		Long:    "Replace action wording and expected result. Omitted expected result is cleared. Assignment and coordination are unchanged; use update-entity-text to preserve the expected result.",
		Example: "  gcx irm opmodel containers update-action INC-123 act-1 --text 'restart checkout' --expected-result 'healthy pods' --actor-kind agent --actor-id codex",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := validateOpModelIDInputs(shared, "action-id", args[1], opts); err != nil {
				return err
			}
			return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
				resp, err := api.EditAction(ctx, editActionRequest{Container: opts.Container, ActionID: args[1], Actor: opts.Actor.actor(), Content: opts.Content.content(opts.Text)})
				if err != nil {
					return opModelMutation{}, err
				}
				return newOpModelMutation("update-action", opts.Container, opModelTarget{Kind: "action", ID: args[1]}, nil, resp.Revision), nil
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

type claimsRecoveryOpts struct {
	IO        cmdio.Options
	Container string
	Recovery  string
	Reason    string
	Actor     opModelActorOpts
}

func (o *claimsRecoveryOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Recovery, "recovery", "", "Impact recovery: unknown, ongoing, mitigated, ended (required)")
	flags.StringVar(&o.Reason, "reason", "", "Reason for recovery judgment (required for mitigated; describe comparison and remaining harm)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}
func (o *claimsRecoveryOpts) Validate() error {
	if err := requireOpModelArg("container", o.Container); err != nil {
		return err
	}
	if err := validateEnumFlag("recovery", o.Recovery, recoveryVocab); err != nil {
		return err
	}
	if o.Recovery == "mitigated" && strings.TrimSpace(o.Reason) == "" {
		return opModelUsage("--reason is required for mitigated recovery; describe comparison and remaining harm")
	}
	return o.Actor.validate()
}
func newClaimsRecoveryCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &claimsRecoveryOpts{}
	cmd := &cobra.Command{
		Use:     "update-claim-recovery <container> <claim-id>",
		Short:   "Judge impact recovery independently of assessment (human only).",
		Long:    "Record recovery against the impact's current content at write time. This does not confirm the impact; completing an action does not establish recovery.",
		Example: "  gcx irm opmodel containers update-claim-recovery INC-123 clm-1 --recovery ended --reason 'harm no longer observed' --actor-kind human --actor-id responder",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Container = args[0]
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := validateOpModelIDInputs(shared, "claim-id", args[1], opts); err != nil {
				return err
			}
			return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
				resp, err := api.UpdateRecovery(ctx, updateRecoveryRequest{Container: opts.Container, ClaimID: args[1], Actor: opts.Actor.actor(), Recovery: opts.Recovery, Reason: opts.Reason})
				if err != nil {
					return opModelMutation{}, err
				}
				return newOpModelMutation("update-claim-recovery", opts.Container, opModelTarget{Kind: "claim", ID: args[1]}, map[string]string{"recovery": opts.Recovery}, resp.Revision), nil
			})
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}
