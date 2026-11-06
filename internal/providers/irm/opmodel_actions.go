// opmodel_actions.go; `gcx irm opmodel actions` leaves: create (record
// operational work outright) and coordinate (report status or assignment).
package irm

import (
	"context"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newOpModelActionsCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actions",
		Short: "Record and coordinate operational work.",
	}
	cmd.AddCommand(
		newActionsCreateCmd(loader, shared),
		newActionsEditCmd(loader, shared),
		newActionsCoordinateCmd(loader, shared),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// actions create
// ---------------------------------------------------------------------------

type actionsCreateOpts struct {
	Content   actionContentOpts
	Assignee  assigneeOpts
	IO        cmdio.Options
	Container string
	Text      string
	Actor     opModelActorOpts
}

func (o *actionsCreateOpts) setup(flags *pflag.FlagSet) {
	o.Content.bind(flags)
	o.Assignee.bind(flags, false)
	bindOpModelContainerFlag(flags, &o.Container)
	flags.StringVar(&o.Text, "text", "", "The work being done (required)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *actionsCreateOpts) Validate() error {
	if err := o.Assignee.validate(); err != nil {
		return err
	}
	if err := requireOpModelFlag("container", o.Container); err != nil {
		return err
	}
	if err := requireOpModelFlag("text", o.Text); err != nil {
		return err
	}
	return o.Actor.validate()
}

func newActionsCreateCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &actionsCreateOpts{}
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Record a piece of operational work (starts proposed).",
		Long: `Record an action: operational work, not a claim about the world.
Evidence does not bear on it and any actor may report its status. The
initial status is proposed.`,
		Example: `  gcx irm opmodel actions create --container INC-123 \
    --text "restart the checkout pods" --actor-kind agent --actor-id claude-code`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runActionsCreate(cmd, opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runActionsCreate(cmd *cobra.Command, opts *actionsCreateOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelInputs(shared, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		resp, err := api.CreateAction(ctx, createActionRequest{
			ExpectedResult: opts.Content.content(opts.Text).ExpectedResult, Assignee: opts.Assignee.actor(),
			Container: opts.Container,
			Actor:     opts.Actor.actor(),
			Text:      opts.Text,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("create", opts.Container,
			opModelTarget{Kind: "action", ID: resp.ActionID},
			map[string]string{"status": resp.Status},
			resp.Revision), nil
	})
}

// ---------------------------------------------------------------------------
// actions coordinate
// ---------------------------------------------------------------------------

type actionsCoordinateOpts struct {
	Assignee  assigneeOpts
	flags     *pflag.FlagSet
	IO        cmdio.Options
	Container string
	Status    string
	Reason    string
	Actor     opModelActorOpts
}

func (o *actionsCoordinateOpts) setup(flags *pflag.FlagSet) {
	o.Assignee.bind(flags, true)
	o.flags = flags
	bindOpModelContainerFlag(flags, &o.Container)
	flags.StringVar(&o.Status, "status", "", "Workflow status: proposed, accepted, in_progress, blocked, done, failed, abandoned (omission preserves status)")
	flags.StringVar(&o.Reason, "reason", "", "Reason for the status or assignment update (optional)")
	o.Actor.bind(flags)
	setupOpModelMutationIO(&o.IO, flags)
}

func (o *actionsCoordinateOpts) Validate() error {
	if err := requireOpModelFlag("container", o.Container); err != nil {
		return err
	}
	if err := o.Assignee.validate(); err != nil {
		return err
	}
	if o.flags.Changed("status") {
		if err := validateEnumFlag("status", o.Status, actionStatusVocab); err != nil {
			return err
		}
	} else if !o.Assignee.Clear && !o.Assignee.supplied() {
		return opModelUsage("supply --status, assignee fields, or --clear-assignee")
	}
	return o.Actor.validate()
}

func newActionsCoordinateCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &actionsCoordinateOpts{}
	cmd := &cobra.Command{
		Use:   "coordinate <action-id>",
		Short: "Report an action's workflow status or assignment (any actor).",
		Long: `Report action status or assignment against current state at write time.
Omitted fields are preserved; --clear-assignee explicitly clears assignment.
This reports work, grants no execution permission, and does not judge recovery.`,
		Example: `  gcx irm opmodel actions coordinate act-3 --container INC-123 \
    --status done --reason "pods healthy" --actor-kind agent --actor-id claude-code`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runActionsCoordinate(cmd, args[0], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return markExperimental(cmd)
}

func runActionsCoordinate(cmd *cobra.Command, actionID string, opts *actionsCoordinateOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "action-id", actionID, opts); err != nil {
		return err
	}
	return runOpModelMutation(cmd, loader, shared, &opts.IO, func(ctx context.Context, api OpModelAPI) (opModelMutation, error) {
		var status *string
		if cmd.Flags().Changed("status") {
			status = &opts.Status
		}
		resp, err := api.CoordinateAction(ctx, coordinateActionRequest{
			Container: opts.Container,
			Actor:     opts.Actor.actor(),
			ActionID:  actionID,
			Status:    status, Assignee: opts.Assignee.actor(), SetAssignee: opts.Assignee.Clear || opts.Assignee.supplied(),
			Reason: opts.Reason,
		})
		if err != nil {
			return opModelMutation{}, err
		}
		return newOpModelMutation("coordinate", opts.Container,
			opModelTarget{Kind: "action", ID: actionID},
			nil,
			resp.Revision), nil
	})
}
