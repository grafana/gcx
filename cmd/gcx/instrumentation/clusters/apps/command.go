// Package apps provides the "gcx instrumentation clusters apps" subcommand tree:
// list, get, configure, remove, wait — all scoped to a single cluster's
// namespace-level Beyla instrumentation configuration.
//
// The apps tree operates on:
//   - Declared state:  GetAppInstrumentation / SetAppInstrumentation (per-cluster blob)
//   - Observed state:  RunK8sDiscovery (fleet-wide workload status, filtered client-side)
//
// Read-modify-write (RMW) operations use the rmw.Update helper (T4) with AppEqual
// as the equality function for client-side optimistic-lock detection.
// All write operations preserve other namespaces' entries byte-equal.
package apps

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/spf13/cobra"
)

// appsClient is the minimal interface required by this package's commands.
// The real *instrumentation.Client satisfies this interface; tests use fakes.
type appsClient interface {
	GetAppInstrumentation(ctx context.Context, clusterName string) (*instrumentation.GetAppInstrumentationResponse, error)
	SetAppInstrumentation(ctx context.Context, clusterName string, namespaces []instrumentation.App, urls instrumentation.BackendURLs) error
	RunK8sDiscovery(ctx context.Context) (*instrumentation.RunK8sDiscoveryResponse, error)
	IsNamespaceDiscovered(ctx context.Context, cluster, namespace string) (bool, error)
	ListPipelines(ctx context.Context) ([]instrumentation.Pipeline, error)
}

// appClientFactory is a deferred client constructor. It is called inside each
// command's RunE — after cobra has parsed all flags (including --context) — so
// client construction is always lazy and never happens at Command() call time.
//
// Returning both values from a single call ensures a single
// fleet.LoadClientWithStack round-trip per command invocation.
type appClientFactory = func(ctx context.Context) (appsClient, instrumentation.BackendURLs, error)

// factoryFromLoader returns an appClientFactory that lazily constructs the
// instrumentation client from the given fleet.ConfigLoader.
//
// The loader is captured eagerly (at Command() call time), but the actual client
// construction (fleet.LoadClientWithStack call) is deferred until the returned
// function is invoked inside a command's RunE.
func factoryFromLoader(loader fleet.ConfigLoader) appClientFactory {
	return func(ctx context.Context) (appsClient, instrumentation.BackendURLs, error) {
		r, err := fleet.LoadClientWithStack(ctx, loader)
		if err != nil {
			return nil, instrumentation.BackendURLs{}, fmt.Errorf("apps: %w", err)
		}
		return instrumentation.NewClient(r.Client),
			instrumentation.BackendURLsFromStack(r.Stack), nil
	}
}

// Command returns the cobra parent command for the apps subtree.
// Subcommands: list, get, configure, remove, wait.
// The command is registered into clusters/command.go.
//
// loader is used to lazily construct the instrumentation client inside each
// subcommand's RunE, after the --context flag has been parsed by cobra.
func Command(loader fleet.ConfigLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apps",
		Short: "Manage namespace-level Beyla instrumentation for a cluster",
		Long: `Manage namespace-level Beyla auto-instrumentation configuration for a cluster.

Commands operate on the declared state stored in Grafana Cloud's
instrumentation service (GetAppInstrumentation / SetAppInstrumentation).
The wait command reads observed state from RunK8sDiscovery.

Identity is positional: configure and remove take <cluster> and <namespace> as
the first two positional arguments.`,
	}

	factory := factoryFromLoader(loader)

	cmd.AddCommand(makeListCmd(factory))
	cmd.AddCommand(makeGetCmd(factory))
	cmd.AddCommand(makeConfigureCmd(factory))
	cmd.AddCommand(makeRemoveCmd(factory))
	cmd.AddCommand(makeWaitCmd(factory))

	return cmd
}
