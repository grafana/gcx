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
		Use: "edit <observation-id>", Short: "Replace observation content, clearing omitted optional fields.",
		Long:    "Replace text, source, observed interval, scope, and original author. Omitted optional values are cleared. Use entities edit-text to preserve context while changing wording.",
		Example: "  gcx irm opmodel observations edit obs-1 --container INC-123 --text 'corrected observation' --actor-kind human --actor-id responder",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := validateOpModelIDInputs(shared, "observation-id", args[0], opts); err != nil {
				return err
			}
			return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
				resp, err := api.EditObservation(ctx, editObservationRequest{Container: opts.Container, ObservationID: args[0], Actor: opts.Actor.actor(), Content: opts.Context.content(opts.Text, opts.SourceRef)})
				if err != nil {
					return opModelMutation{}, err
				}
				return newOpModelMutation("edit", opts.Container, opModelTarget{Kind: "observation", ID: args[0]}, nil, resp.Revision), nil
			})
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

type actionsEditOpts struct {
	IO        cmdio.Options
	Container string
	Text      string
	Content   actionContentOpts
	Actor     opModelActorOpts
}

func (o *actionsEditOpts) setup(flags *pflag.FlagSet) {
	bindOpModelContainerFlag(flags, &o.Container)
	flags.StringVar(&o.Text, "text", "", "Replacement action wording (required)")
	o.Content.bind(flags)
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}
func (o *actionsEditOpts) Validate() error {
	if err := requireOpModelFlag("container", o.Container); err != nil {
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
		Use: "edit <action-id>", Short: "Replace action content, clearing an omitted expected result.",
		Long:    "Replace action wording and expected result. Omitted expected result is cleared. Assignment and coordination are unchanged; use entities edit-text to preserve the expected result.",
		Example: "  gcx irm opmodel actions edit act-1 --container INC-123 --text 'restart checkout' --expected-result 'healthy pods' --actor-kind agent --actor-id codex",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := validateOpModelIDInputs(shared, "action-id", args[0], opts); err != nil {
				return err
			}
			return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
				resp, err := api.EditAction(ctx, editActionRequest{Container: opts.Container, ActionID: args[0], Actor: opts.Actor.actor(), Content: opts.Content.content(opts.Text)})
				if err != nil {
					return opModelMutation{}, err
				}
				return newOpModelMutation("edit", opts.Container, opModelTarget{Kind: "action", ID: args[0]}, nil, resp.Revision), nil
			})
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

type claimsRecoveryOpts struct {
	IO        cmdio.Options
	Container string
	Recovery  string
	Reason    string
	Actor     opModelActorOpts
}

func (o *claimsRecoveryOpts) setup(flags *pflag.FlagSet) {
	bindOpModelContainerFlag(flags, &o.Container)
	flags.StringVar(&o.Recovery, "recovery", "", "Impact recovery: unknown, ongoing, mitigated, ended (required)")
	flags.StringVar(&o.Reason, "reason", "", "Reason for recovery judgment (required for mitigated; describe comparison and remaining harm)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}
func (o *claimsRecoveryOpts) Validate() error {
	if err := requireOpModelFlag("container", o.Container); err != nil {
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
		Use: "recovery <claim-id>", Short: "Judge impact recovery independently of assessment (human only).",
		Long:    "Record recovery against the impact's current content at write time. This does not confirm the impact; completing an action does not establish recovery.",
		Example: "  gcx irm opmodel claims recovery clm-1 --container INC-123 --recovery ended --reason 'harm no longer observed' --actor-kind human --actor-id responder",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			if err := validateOpModelIDInputs(shared, "claim-id", args[0], opts); err != nil {
				return err
			}
			return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
				resp, err := api.UpdateRecovery(ctx, updateRecoveryRequest{Container: opts.Container, ClaimID: args[0], Actor: opts.Actor.actor(), Recovery: opts.Recovery, Reason: opts.Reason})
				if err != nil {
					return opModelMutation{}, err
				}
				return newOpModelMutation("recovery", opts.Container, opModelTarget{Kind: "claim", ID: args[0]}, map[string]string{"recovery": opts.Recovery}, resp.Revision), nil
			})
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}
