package services

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instrumout "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type listOpts struct {
	Cluster   string
	Namespace string
	Status    string
	All       bool
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	flags.StringVar(&o.Cluster, "cluster", "", "Filter by cluster name")
	flags.StringVarP(&o.Namespace, "namespace", "n", "", "Filter by Kubernetes namespace")
	flags.StringVar(&o.Status, "status", "", "Filter by instrumentation status (e.g. ERROR, INSTRUMENTED)")
	flags.BoolVarP(&o.All, "all", "A", false, "Show services from all namespaces (fleet-wide)")
}

func (o *listOpts) Validate() error {
	return nil
}

func newListCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	outOpts := &cmdio.Options{}
	outOpts.DefaultFormat("text")
	outOpts.RegisterCustomCodec("text", &instrumout.ServiceTableCodec{Wide: false})
	outOpts.RegisterCustomCodec("wide", &instrumout.ServiceTableCodec{Wide: true})
	outOpts.SetJSONFieldValidator(cmdio.MakeFieldValidator(instrumout.ServiceView{}))

	cmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List discovered workloads across the fleet",
		Long: `List all workloads discovered by the Beyla survey collector.

Calls RunK8sDiscovery() (fleet-wide RPC) and applies client-side filters.

Examples:
  # List all workloads
  gcx instrumentation services list

  # Filter to a specific cluster
  gcx instrumentation services list --cluster prod-1

  # Filter to a specific namespace
  gcx instrumentation services list --namespace checkout

  # Show only workloads in terminal error state
  gcx instrumentation services list --status=ERROR

  # Wide output with extra columns
  gcx instrumentation services list -o wide

Workload-level Selection / override state is not surfaced on this command. To inspect a workload's override, run "gcx instrumentation clusters apps list --cluster=<cluster> --namespace=<namespace>".`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := outOpts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("services list: %w", err)
			}
			client := instrumentation.NewClient(r.Client)
			promHeaders := instrumentation.PromHeadersFromStack(r.Stack)
			return runList(ctx, opts, outOpts, client, promHeaders, cmd.OutOrStdout())
		},
	}

	flags := cmd.Flags()
	opts.setup(flags)
	outOpts.BindFlags(flags)

	return cmd
}

// runList performs the RunK8sDiscovery call and applies client-side filters.
// Separated from RunE for testability.
func runList(
	ctx context.Context,
	opts *listOpts,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	promHeaders instrumentation.PromHeaders,
	out io.Writer,
) error {
	resp, err := client.RunK8sDiscovery(ctx, promHeaders)
	if err != nil {
		return fmt.Errorf("services list: %w", err)
	}

	// Resolve the status filter (supports short alias "ERROR" → INSTRUMENTATION_ERROR).
	var statusFilter instrumentation.InstrumentationStatus
	if opts.Status != "" {
		statusFilter = normalizeStatus(opts.Status)
	}

	// Apply client-side filters and convert to ServiceView.
	// Initialise with make(..., 0) to guarantee [] output in JSON.
	views := make([]instrumout.ServiceView, 0, len(resp.Items))
	for _, item := range resp.Items {
		if opts.Cluster != "" && item.ClusterName != opts.Cluster {
			continue
		}
		// --all overrides --namespace: when set, namespace scoping is disabled and
		// the full fleet-wide result is returned regardless of --namespace value.
		if opts.Namespace != "" && !opts.All && item.Namespace != opts.Namespace {
			continue
		}
		if statusFilter != "" && item.InstrumentationStatus != statusFilter {
			continue
		}
		views = append(views, instrumout.ServiceView{
			ClusterName:           item.ClusterName,
			Namespace:             item.Namespace,
			Name:                  item.Name,
			WorkloadType:          item.WorkloadType,
			DisplayNamespace:      item.DisplayNamespace,
			DisplayName:           item.DisplayName,
			OS:                    item.OS,
			Lang:                  item.Lang,
			InstrumentationStatus: item.InstrumentationStatus,
		})
	}

	return outOpts.Encode(out, instrumout.ServiceListEnvelope{Items: views})
}
