//nolint:dupl // clear, include and exclude are intentionally symmetric DWIM commands; duplication is acceptable here.
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

// clearOpts holds flag-bound options for the "services clear" command.
type clearOpts struct {
	IO cmdio.Options
}

func (o *clearOpts) setup(flags *pflag.FlagSet) {
	// The clear result is a MutationResult document through the codec
	// system: the default text codec prints the familiar one-liner; agent
	// mode and explicit -o json/yaml get the structured document.
	instoutput.BindMutationIO(&o.IO, flags)
}

func (o *clearOpts) Validate() error { return o.IO.Validate() }

func newClearCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &clearOpts{}
	cmd := &cobra.Command{
		Use:   "clear <cluster> <namespace> <service>",
		Short: "Remove a per-workload override, inheriting namespace default",
		Long: `Remove any per-workload inclusion or exclusion override for a workload.

After clearing, the workload inherits the namespace autoinstrument default.

The operation is idempotent: if no override exists for the workload,
the command exits 0 without making any backend calls.

The write uses an optimistic-lock guard (rmw.Update) when a change is needed:
if the namespace list changes between the initial read and the pre-write re-check,
the command returns a conflict error and must be retried.

Examples:
  gcx instrumentation services clear prod-1 checkout frontend`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("services clear: %w", err)
			}
			client := instrumentation.NewClient(r.Client)
			urls := instrumentation.BackendURLsFromStack(r.Stack)
			return runClear(ctx, &opts.IO, client, args[0], args[1], args[2], urls, instrumentation.PromHeadersFromStack(r.Stack), cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runClear removes any per-workload override for the given service.
// If the namespace is not found or has no override for the service, it returns
// nil (idempotent no-op) without making any backend call.
func runClear(
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

	// Idempotence pre-check: if the namespace doesn't exist or has no override,
	// there is nothing to clear.
	resp, err := client.GetAppInstrumentation(ctx, cluster)
	if err != nil {
		return fmt.Errorf("services clear: %w", err)
	}

	ns := findNamespace(resp.Namespaces, namespace)
	if ns == nil {
		// Namespace not configured — nothing to clear; idempotent no-op.
		return outOpts.Encode(out, instoutput.NewMutationResult("clear",
			instoutput.Target{Cluster: cluster, Namespace: namespace, Service: service}))
	}

	proposed := applyClearMutation(*ns, service)
	equal, _ := rmw.AppEqual(*ns, proposed)
	if equal {
		// No override exists for the service — idempotent no-op.
		return outOpts.Encode(out, instoutput.NewMutationResult("clear",
			instoutput.Target{Cluster: cluster, Namespace: namespace, Service: service}))
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
			return applyClearMutation(n, service)
		})
	}
	setFn := func(ctx context.Context, namespaces []instrumentation.App) error {
		return client.SetAppInstrumentation(ctx, cluster, namespaces, urls)
	}

	if err := rmw.Update(ctx, getFn, mutateFn, setFn, namespacesEqual, 3); err != nil {
		var ce rmw.ConflictError
		if errors.As(err, &ce) {
			ce.Command = "services clear"
			ce.Namespace = namespace
			return ce
		}
		return fmt.Errorf("services clear: %w", err)
	}

	result := instoutput.NewMutationResult("clear",
		instoutput.Target{Cluster: cluster, Namespace: namespace, Service: service})
	result.Changed = true
	return outOpts.Encode(out, result)
}
