package clusters

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

// defaultClusterConfig returns the default K8s monitoring configuration.
// defaults: costMetrics=true, clusterEvents=true, energyMetrics=false, nodeLogs=false,
// Selection=SELECTION_INCLUDED.
func defaultClusterConfig(clusterName string) instrumentation.Cluster {
	t := true
	f := false
	return instrumentation.Cluster{
		Name:          clusterName,
		Selection:     "SELECTION_INCLUDED",
		CostMetrics:   &t,
		EnergyMetrics: &f,
		ClusterEvents: &t,
		NodeLogs:      &f,
	}
}

type configureOpts struct {
	IO            cmdio.Options
	useDefaults   bool
	yes           bool
	costMetrics   bool
	energyMetrics bool
	clusterEvents bool
	nodeLogs      bool
}

// Validate of the mutually-exclusive-mode and --yes-required-for-defaults rules
// requires inspecting flags.Changed(), which depends on the *cobra.Command
// instance. Those checks live in runConfigure where the command is in scope.
func (o *configureOpts) Validate() error { return o.IO.Validate() }

func (o *configureOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.useDefaults, "use-defaults", false,
		"Apply canonical defaults (costMetrics=true, clusterEvents=true, energyMetrics=false, nodeLogs=false). Requires --yes.")
	flags.BoolVar(&o.yes, "yes", false,
		"Confirm the --use-defaults operation (required with --use-defaults)")
	flags.BoolVar(&o.costMetrics, "cost-metrics", false,
		"Set costMetrics. Pass --cost-metrics=false to disable. Omit to preserve current value.")
	flags.BoolVar(&o.energyMetrics, "energy-metrics", false,
		"Set energyMetrics. Pass --energy-metrics=false to disable.")
	flags.BoolVar(&o.clusterEvents, "cluster-events", false,
		"Set clusterEvents. Pass --cluster-events=false to disable.")
	flags.BoolVar(&o.nodeLogs, "node-logs", false,
		"Set nodeLogs. Pass --node-logs=false to disable.")
	// The configure result is a MutationResult document through the codec
	// system: the default text codec prints the familiar one-liner; agent
	// mode and explicit -o json/yaml get the structured document.
	instoutput.BindMutationIO(&o.IO, flags)
}

func newConfigureCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &configureOpts{}
	cmd := &cobra.Command{
		Use:   "configure <cluster>",
		Short: "Configure K8s monitoring flags on a cluster",
		Long: `Configure K8s monitoring feature flags on a cluster.

Two mutually exclusive modes:

  --use-defaults --yes
      Apply canonical defaults, overwriting current state. Requires --yes.
      Defaults: costMetrics=true, clusterEvents=true, energyMetrics=false, nodeLogs=false.

  --<feat>[=true|false] (one or more)
      Set listed features; unspecified features preserve their current value (RMW).
      Idempotent. No confirmation required.

Combining --use-defaults with any --<feat> flag is an error.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Apply defaults to cluster "prod-eu"
  gcx instrumentation clusters configure prod-eu --use-defaults --yes

  # Enable cost metrics, preserving other flags (RMW)
  gcx instrumentation clusters configure prod-eu --cost-metrics

  # Disable node logs (RMW)
  gcx instrumentation clusters configure prod-eu --node-logs=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			clusterName := args[0]
			if err := opts.Validate(); err != nil {
				return err
			}
			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("clusters configure: %w", err)
			}
			client := instrumentation.NewClient(r.Client)
			backendURLs := instrumentation.BackendURLsFromStack(r.Stack)
			return runConfigure(ctx, cmd, opts, client, clusterName, backendURLs, cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runConfigure performs the configure operation. It supports two mutually
// exclusive modes (--use-defaults --yes vs. one or more --<feat> flags).
// cmd is needed to inspect Flags().Changed() for distinguishing "set to false"
// from "not set" semantics.
func runConfigure(
	ctx context.Context,
	cmd *cobra.Command,
	opts *configureOpts,
	client clusterClient,
	clusterName string,
	backendURLs instrumentation.BackendURLs,
	w io.Writer,
) error {
	flags := cmd.Flags()
	useDefaultsSet := flags.Changed("use-defaults")
	anyFeatureSet := flags.Changed("cost-metrics") || flags.Changed("energy-metrics") ||
		flags.Changed("cluster-events") || flags.Changed("node-logs")

	if useDefaultsSet && anyFeatureSet {
		return errors.New("clusters configure: --use-defaults and feature flags are mutually exclusive; " +
			"use one or more --<feat> flags for incremental edit, or --use-defaults --yes to apply defaults")
	}
	if !useDefaultsSet && !anyFeatureSet {
		return errors.New("clusters configure: requires either --use-defaults --yes (apply defaults) " +
			"or one or more --<feat> flags (incremental edit)")
	}

	// --use-defaults mode: destructive overwrite, requires --yes.
	if useDefaultsSet {
		if !opts.yes {
			return errors.New("clusters configure --use-defaults: --yes is required to confirm this destructive operation")
		}
		defaults := defaultClusterConfig(clusterName)
		if err := client.SetK8SInstrumentation(ctx, clusterName, defaults, backendURLs); err != nil {
			return fmt.Errorf("clusters configure: %w", err)
		}
		result := instoutput.NewMutationResult("configure", instoutput.Target{Cluster: clusterName})
		result.Changed = true
		result.Fields = []instoutput.FieldChange{{Name: "use-defaults", From: "custom", To: "defaults"}}
		return opts.IO.Encode(w, result)
	}

	// RMW mode: set listed flags, preserve unspecified.
	getFn := func(ctx context.Context) (instrumentation.Cluster, error) {
		resp, err := client.GetK8SInstrumentation(ctx, clusterName)
		if err != nil {
			return instrumentation.Cluster{}, fmt.Errorf("clusters configure: %w", err)
		}
		return resp.Cluster, nil
	}
	mutateFn := func(current instrumentation.Cluster) instrumentation.Cluster {
		mutated := current
		mutated.Selection = "SELECTION_INCLUDED"
		if flags.Changed("cost-metrics") {
			mutated.CostMetrics = boolPtr(opts.costMetrics) //nolint:modernize
		}
		if flags.Changed("energy-metrics") {
			mutated.EnergyMetrics = boolPtr(opts.energyMetrics) //nolint:modernize
		}
		if flags.Changed("cluster-events") {
			mutated.ClusterEvents = boolPtr(opts.clusterEvents) //nolint:modernize
		}
		if flags.Changed("node-logs") {
			mutated.NodeLogs = boolPtr(opts.nodeLogs) //nolint:modernize
		}
		return mutated
	}
	setFn := func(ctx context.Context, updated instrumentation.Cluster) error {
		return client.SetK8SInstrumentation(ctx, clusterName, updated, backendURLs)
	}

	// Pre-check: read current state and compute the proposed post-state.
	// If they are equal, skip the write and report changed:false. This
	// makes idempotent re-runs observable without a redundant API call.
	// (Note: rmw.Update also performs a GET internally; the extra GET here is
	// an intentional trade-off for accurate no-op detection before writing.)
	preResp, err := client.GetK8SInstrumentation(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("clusters configure: %w", err)
	}
	post := mutateFn(preResp.Cluster)
	if equal, _ := rmw.ClusterEqual(preResp.Cluster, post); equal {
		return opts.IO.Encode(w, instoutput.NewMutationResult("configure", instoutput.Target{Cluster: clusterName}))
	}

	err = rmw.Update[instrumentation.Cluster](ctx, getFn, mutateFn, setFn, rmw.ClusterEqual, 2)
	if err != nil {
		var ce rmw.ConflictError
		if errors.As(err, &ce) {
			ce.Command = "clusters configure"
			return ce
		}
		return err
	}
	result := instoutput.NewMutationResult("configure", instoutput.Target{Cluster: clusterName})
	result.Changed = true
	return opts.IO.Encode(w, result)
}
