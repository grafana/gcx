//nolint:dupl // exclude and include are intentionally symmetric; duplication is acceptable here.
package services

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/grafana/gcx/internal/providers/instrumentation/rmw"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// excludeOpts holds flag-bound options for the "services exclude" command.
// The command takes only positional args today, so the struct is empty;
// it exists to satisfy the canonical opts struct + setup + Validate pattern.
type excludeOpts struct{}

func (o *excludeOpts) setup(_ *pflag.FlagSet) {}

func (o *excludeOpts) Validate() error { return nil }

func newExcludeCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &excludeOpts{}
	cmd := &cobra.Command{
		Use:   "exclude <cluster> <namespace> <service>",
		Short: "Exclude a workload from instrumentation (DWIM, idempotent)",
		Long: `Exclude a specific workload from instrumentation using DWIM semantics.

DWIM logic:
  - Removes any existing INCLUDED override for the workload.
  - Adds an EXCLUDED override iff the namespace autoinstrument is explicitly
    true (i.e. the namespace default is on, so an explicit opt-out is needed).
  - If the namespace autoinstrument is false/nil, no override is added (the
    namespace default is already off — adding EXCLUDED would be redundant).

The operation is idempotent: running it twice with the same args exits 0 and
the second call is a no-op against the backend.

The write uses an optimistic-lock guard (rmw.Update): if the namespace list
changes between the initial read and the pre-write re-check, the command returns
a conflict error and must be retried.

Examples:
  gcx instrumentation services exclude prod-1 checkout payment-svc`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("services exclude: %w", err)
			}
			client := instrumentation.NewClient(r.Client)
			urls := instrumentation.BackendURLsFromStack(r.Stack)
			return runExclude(ctx, client, args[0], args[1], args[2], urls, instrumentation.PromHeadersFromStack(r.Stack), cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runExclude executes the DWIM exclude operation for a single workload.
// Symmetric to runInclude (see include.go for full design notes).
func runExclude(
	ctx context.Context,
	client *instrumentation.Client,
	cluster, namespace, service string,
	urls instrumentation.BackendURLs,
	promHeaders instrumentation.PromHeaders,
	out io.Writer,
) error {
	// Workload existence pre-flight: verify the service appears in discovery.
	if err := validateWorkloadExists(ctx, client, promHeaders, cluster, namespace, service); err != nil {
		return err
	}

	// Idempotence pre-check.
	resp, err := client.GetAppInstrumentation(ctx, cluster)
	if err != nil {
		return fmt.Errorf("services exclude: %w", err)
	}

	ns := findNamespace(resp.Namespaces, namespace)
	if ns == nil {
		return fmt.Errorf("services exclude: namespace %q not found in cluster %q; "+
			"run 'gcx instrumentation clusters apps enable %s %s' first",
			namespace, cluster, cluster, namespace)
	}

	proposed := applyExcludeMutation(*ns, service)
	equal, _ := rmw.AppEqual(*ns, proposed)
	if equal {
		// Already in the desired state — idempotent no-op.
		return instoutput.MutationResult{
			Action:  "exclude",
			Target:  instoutput.Target{Cluster: cluster, Namespace: namespace, Service: service},
			Changed: false,
		}.Emit(out)
	}

	getFn := func(ctx context.Context) ([]instrumentation.App, error) {
		r, err := client.GetAppInstrumentation(ctx, cluster)
		if err != nil {
			return nil, err
		}
		return r.Namespaces, nil
	}
	mutateFn := func(namespaces []instrumentation.App) []instrumentation.App {
		return applyMutationToNamespaces(namespaces, namespace, func(n instrumentation.App) instrumentation.App {
			return applyExcludeMutation(n, service)
		})
	}
	setFn := func(ctx context.Context, namespaces []instrumentation.App) error {
		return client.SetAppInstrumentation(ctx, cluster, namespaces, urls)
	}

	if err := rmw.Update(ctx, getFn, mutateFn, setFn, namespacesEqual, 3); err != nil {
		var ce rmw.ConflictError
		if errors.As(err, &ce) {
			ce.Command = "services exclude"
			ce.Namespace = namespace
			return ce
		}
		return fmt.Errorf("services exclude: %w", err)
	}

	return instoutput.MutationResult{
		Action:  "exclude",
		Target:  instoutput.Target{Cluster: cluster, Namespace: namespace, Service: service},
		Changed: true,
	}.Emit(out)
}
