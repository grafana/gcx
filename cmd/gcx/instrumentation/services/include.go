//nolint:dupl // include and exclude are intentionally symmetric; duplication is acceptable here.
package services

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/grafana/gcx/internal/providers/instrumentation/rmw"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// includeOpts holds flag-bound options for the "services include" command.
type includeOpts struct {
	IO cmdio.Options
}

func (o *includeOpts) setup(flags *pflag.FlagSet) {
	// The include result is a MutationResult document through the codec
	// system: the default text codec prints the familiar one-liner; agent
	// mode and explicit -o json/yaml get the structured document.
	instoutput.BindMutationIO(&o.IO, flags)
}

func (o *includeOpts) Validate() error { return o.IO.Validate() }

func newIncludeCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &includeOpts{}
	cmd := &cobra.Command{
		Use:   "include <cluster> <namespace> <service>",
		Short: "Include a workload for instrumentation (DWIM, idempotent)",
		Long: `Include a specific workload for instrumentation using DWIM semantics.

DWIM logic:
  - Removes any existing EXCLUDED override for the workload.
  - Adds an INCLUDED override iff the namespace autoinstrument is NOT explicitly
    true (i.e. the namespace default is off, so an explicit opt-in is needed).
  - If the namespace autoinstrument is true, no override is added (namespace
    default is already on — adding INCLUDED would be redundant).

The operation is idempotent: running it twice with the same args exits 0
and the second call is a no-op against the backend.

The write uses an optimistic-lock guard (rmw.Update): if the namespace list
changes between the initial read and the pre-write re-check, the command returns
a conflict error and must be retried.

Examples:
  gcx instrumentation services include prod-1 checkout frontend`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("services include: %w", err)
			}
			client := instrumentation.NewClient(r.Client)
			urls := instrumentation.BackendURLsFromStack(r.Stack)
			return runInclude(ctx, &opts.IO, client, args[0], args[1], args[2], urls, instrumentation.PromHeadersFromStack(r.Stack), cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runInclude executes the DWIM include operation for a single workload.
// It first performs an idempotence pre-check: if the proposed mutation equals
// the current state, it returns nil immediately (zero Set calls). Otherwise it
// calls rmw.Update which handles retries and optimistic-lock detection.
func runInclude(
	ctx context.Context,
	outOpts *cmdio.Options,
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

	// Idempotence pre-check: read current state, compute mutation, compare.
	// If already in the desired state, return nil without calling Set.
	resp, err := client.GetAppInstrumentation(ctx, cluster)
	if err != nil {
		return fmt.Errorf("services include: %w", err)
	}

	ns := findNamespace(resp.Namespaces, namespace)
	if ns == nil {
		return fmt.Errorf("services include: namespace %q not found in cluster %q; "+
			"run 'gcx instrumentation clusters apps enable %s %s' first",
			namespace, cluster, cluster, namespace)
	}

	proposed := applyIncludeMutation(*ns, service)
	equal, _ := rmw.AppEqual(*ns, proposed)
	if equal {
		// Already in the desired state — idempotent no-op; exit 0 with no Set call.
		return outOpts.Encode(out, instoutput.NewMutationResult("include",
			instoutput.Target{Cluster: cluster, Namespace: namespace, Service: service}))
	}

	// Not a no-op: run the full RMW with optimistic-lock guard.
	getFn := func(ctx context.Context) ([]instrumentation.App, error) {
		r, err := client.GetAppInstrumentation(ctx, cluster)
		if err != nil {
			return nil, err
		}
		return r.Namespaces, nil
	}
	mutateFn := func(namespaces []instrumentation.App) []instrumentation.App {
		return applyMutationToNamespaces(namespaces, namespace, func(n instrumentation.App) instrumentation.App {
			return applyIncludeMutation(n, service)
		})
	}
	setFn := func(ctx context.Context, namespaces []instrumentation.App) error {
		return client.SetAppInstrumentation(ctx, cluster, namespaces, urls)
	}

	if err := rmw.Update(ctx, getFn, mutateFn, setFn, namespacesEqual, 3); err != nil {
		var ce rmw.ConflictError
		if errors.As(err, &ce) {
			ce.Command = "services include"
			ce.Namespace = namespace
			return ce
		}
		return fmt.Errorf("services include: %w", err)
	}

	result := instoutput.NewMutationResult("include",
		instoutput.Target{Cluster: cluster, Namespace: namespace, Service: service})
	result.Changed = true
	return outOpts.Encode(out, result)
}
