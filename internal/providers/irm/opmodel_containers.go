package irm

import (
	"context"
	"fmt"

	fail "github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newOpModelContainersCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "containers",
		Short: "Read and change a container's operational model.",
	}
	cmd.AddCommand(
		newContainersGetCmd(loader, shared),
		newContainersListEventsCmd(loader, shared),
		newObservationsCreateCmd(loader, shared),
		newObservationsPromoteCmd(loader, shared),
		newObservationsPromoteToActionCmd(loader, shared),
		newObservationsVerifyCmd(loader, shared),
		newObservationsEditCmd(loader, shared),
		newClaimsCreateCmd(loader, shared),
		newClaimsAssessCmd(loader, shared),
		newClaimsRecoveryCmd(loader, shared),
		newActionsCreateCmd(loader, shared),
		newActionsCoordinateCmd(loader, shared),
		newActionsEditCmd(loader, shared),
		newEntitiesEditTextCmd(loader, shared),
		newEvidenceLinksAttachCmd(loader, shared),
		newEvidenceLinksVerifyCmd(loader, shared),
		newEvidenceLinksWithdrawCmd(loader, shared),
		newContainersUpsertModelCmd(loader, shared),
	)
	return cmd
}

type containersGetOpts struct {
	IO cmdio.Options
}

func (o *containersGetOpts) setup(flags *pflag.FlagSet) {
	setupOpModelReadIO(&o.IO, flags)
}

func (o *containersGetOpts) Validate() error { return nil }

func newContainersGetCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &containersGetOpts{}
	cmd := &cobra.Command{
		Use:   "get <container>",
		Short: "Read a container's folded projection (all entities, current state).",
		Long: `Read the container's folded state: every observation, claim, evidence link,
and action, including content versions, independent judgment dimensions,
attribution, origin, and withdrawal. Judgment freshness covers direct subject
content only, not evidence dependencies. Version bindings record server state at
write time, not proof of what the writer reviewed. Absent judgments remain absent.
The head revision is an opaque stream cursor, not a content version; use it
with list-events --since-revision to fetch subsequent events.`,
		Example: `  gcx irm opmodel containers get INC-123 -o json`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runContainersGet(cmd, args[0], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runContainersGet(cmd *cobra.Command, container string, opts *containersGetOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "container", container, opts); err != nil {
		return err
	}
	return withOpModel(cmd, loader, shared, func(ctx context.Context, api OpModelAPI) error {
		resp, err := api.GetContainer(ctx, getContainerRequest{Container: container})
		if err != nil {
			return err
		}
		return opts.IO.Encode(cmd.OutOrStdout(), newContainerEnvelope(resp))
	})
}

type containersListEventsOpts struct {
	IO            cmdio.Options
	SinceRevision string
	PageSize      int32
}

func (o *containersListEventsOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.SinceRevision, "since-revision", "", "Return only events after this revision (0 = from the beginning)")
	flags.Int32Var(&o.PageSize, "page-size", 0, "Maximum events per call (0 = server default)")
	setupOpModelReadIO(&o.IO, flags)
}

func (o *containersListEventsOpts) Validate() error {
	if o.PageSize < 0 {
		exit2 := fail.ExitUsageError
		return &fail.DetailedError{
			Summary:     fmt.Sprintf("invalid --page-size %d", o.PageSize),
			Details:     "The page size must be >= 0 (0 means the server default).",
			ExitCode:    &exit2,
			Suggestions: []string{"Re-run with --page-size 100"},
		}
	}
	return nil
}

func newContainersListEventsCmd(loader OpModelClientLoader, shared *opModelSharedOpts) *cobra.Command {
	opts := &containersListEventsOpts{}
	cmd := &cobra.Command{
		Use:   "list-events <container>",
		Short: "Read a container's raw event stream, in append order.",
		Long: `Read the container's append-only event stream. The response carries the
container's head revision independent of the page, so you know whether you
have caught up: when the last event's revision differs from the head, re-run
with --since-revision <last-revision> for the rest. Revisions are opaque and may
be sparse. Do not advance a partial page's cursor to the container head.`,
		Example: `  gcx irm opmodel containers list-events INC-123 --since-revision 12 -o json`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return err
			}
			return runContainersListEvents(cmd, args[0], opts, loader, shared)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

func runContainersListEvents(cmd *cobra.Command, container string, opts *containersListEventsOpts, loader OpModelClientLoader, shared *opModelSharedOpts) error {
	if err := validateOpModelIDInputs(shared, "container", container, opts); err != nil {
		return err
	}
	return withOpModel(cmd, loader, shared, func(ctx context.Context, api OpModelAPI) error {
		resp, err := api.ListEvents(ctx, listEventsRequest{
			Container:     container,
			SinceRevision: opts.SinceRevision,
			PageSize:      opts.PageSize,
		})
		if err != nil {
			return err
		}
		env := newOpModelEventsEnvelope(container, resp)
		if err := opts.IO.Encode(cmd.OutOrStdout(), env); err != nil {
			return err
		}
		if n := len(env.Events); n > 0 && env.Events[n-1].Revision != env.Revision {
			cmdio.EmitHint(cmd.ErrOrStderr(), "More events after this page",
				"gcx irm opmodel containers list-events "+container+" --since-revision "+env.Events[n-1].Revision)
		}
		return nil
	})
}
