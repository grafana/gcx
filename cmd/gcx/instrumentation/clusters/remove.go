package clusters

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type removeOpts struct {
	Yes bool
}

func (o *removeOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.Yes, "yes", false, "Confirm the remove operation (required)")
}

func (o *removeOpts) Validate() error {
	if !o.Yes {
		return errors.New("clusters remove: --yes is required to confirm this destructive operation")
	}
	return nil
}

func newRemoveCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &removeOpts{}
	cmd := &cobra.Command{
		Use:   "remove <cluster>",
		Short: "Remove K8s monitoring from a cluster",
		Long: `Remove K8s monitoring from a cluster.

Calls SetK8SInstrumentation with Selection=SELECTION_EXCLUDED. The backend
interprets this as a request to delete the K8s monitoring pipeline for the
cluster.

IMPORTANT: After removing, the cluster's status takes approximately 5 minutes
to transition from INSTRUMENTED to NOT_INSTRUMENTED. During this decay window,
the cluster may still appear as INSTRUMENTED in status output. This is expected
behaviour — the Alloy collector drains its in-flight telemetry before stopping.

Requires --yes to confirm the destructive operation.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Remove K8s monitoring for cluster "prod-eu"
  gcx instrumentation clusters remove prod-eu --yes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			clusterName := args[0]

			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("clusters remove: %w", err)
			}
			client := instrumentation.NewClient(r.Client)
			backendURLs := instrumentation.BackendURLsFromStack(r.Stack)

			return runRemove(ctx, client, clusterName, backendURLs, cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runRemove implements the core remove logic. Separated from newRemoveCommand
// for testability with fake clients.
func runRemove(
	ctx context.Context,
	client clusterClient,
	clusterName string,
	backendURLs instrumentation.BackendURLs,
	w io.Writer,
) error {
	// Single-shot write with Selection=SELECTION_EXCLUDED.
	// Other fields are left at zero — the backend treats SELECTION_EXCLUDED
	// as "delete the pipeline" regardless of other fields.
	excluded := instrumentation.Cluster{
		Name:      clusterName,
		Selection: "SELECTION_EXCLUDED",
	}
	if err := client.SetK8SInstrumentation(ctx, clusterName, excluded, backendURLs); err != nil {
		return fmt.Errorf("clusters remove: %w", err)
	}
	return instoutput.MutationResult{
		Action:  "remove",
		Target:  instoutput.Target{Cluster: clusterName},
		Changed: true,
	}.Emit(w)
}
