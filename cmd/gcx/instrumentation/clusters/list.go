package clusters

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/enumerate"
	instrOutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

type listOpts struct {
	IO cmdio.Options
}

func (o *listOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &instrOutput.ClusterTableCodec{Wide: false})
	o.IO.RegisterCustomCodec("wide", &instrOutput.ClusterTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(instrOutput.ClusterView{}))
	o.IO.BindFlags(flags)
}

func (o *listOpts) Validate() error {
	return o.IO.Validate()
}

func newListCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all clusters with their instrumentation status",
		Long: `List all clusters with their K8s monitoring configuration and observed status.

Merges RunK8sMonitoring with ListPipelines to surface clusters that have been
configured but whose Alloy collector has not yet started reporting (pre-Alloy
clusters appear with PENDING_INSTRUMENTATION status).

For each cluster, the declared configuration is fetched concurrently via
GetK8SInstrumentation (up to 10 concurrent requests).`,
		Example: `  # List all clusters (table output)
  gcx instrumentation clusters list

  # List in wide format (shows all flag columns)
  gcx instrumentation clusters list -o wide

  # Output as JSON
  gcx instrumentation clusters list -o json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("clusters list: %w", err)
			}
			client := instrumentation.NewClient(r.Client)
			promHeaders := instrumentation.PromHeadersFromStack(r.Stack)

			monClient := &monitoringAdapter{client: client, promHeaders: promHeaders}
			pipeClient := &pipelineAdapter{client: client}

			return runList(ctx, opts, monClient, pipeClient, client, cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runList implements the core list logic. Separated from newListCommand for
// testability with fake clients.
func runList(
	ctx context.Context,
	opts *listOpts,
	monClient enumerate.MonitoringClient,
	pipeClient enumerate.PipelineClient,
	instrClient clusterClient,
	w io.Writer,
) error {
	// Enumerate merges RunK8sMonitoring + ListPipelines.
	observed, err := enumerate.Enumerate(ctx, monClient, pipeClient)
	if err != nil {
		return fmt.Errorf("clusters list: enumerate: %w", err)
	}

	// Fan out GetK8SInstrumentation per cluster (cap 10 concurrent).
	clusters := make([]instrumentation.Cluster, len(observed))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(10)
	for i, oc := range observed {
		name := oc.Name
		g.Go(func() error {
			resp, err := instrClient.GetK8SInstrumentation(gctx, name)
			if err != nil {
				return fmt.Errorf("clusters list: GetK8SInstrumentation %q: %w", name, err)
			}
			clusters[i] = resp.Cluster // safe: i is per-iteration scoped (Go 1.22+)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Build []ClusterView merging declared config + observed state.
	views := make([]instrOutput.ClusterView, len(observed))
	for i, oc := range observed {
		cl := clusters[i]
		cv := instrOutput.ClusterView{
			Name:                  cl.Name,
			Selection:             cl.Selection,
			CostMetrics:           cl.CostMetrics,
			EnergyMetrics:         cl.EnergyMetrics,
			ClusterEvents:         cl.ClusterEvents,
			NodeLogs:              cl.NodeLogs,
			InstrumentationStatus: oc.Status,
		}
		// Fallback: use observed name if declared name is empty (edge case).
		if cv.Name == "" {
			cv.Name = oc.Name
		}
		// Merge observed counters when available.
		if oc.State != nil {
			cv.Namespaces = len(oc.State.Namespaces)
			cv.Workloads = oc.State.Workloads
			cv.Pods = oc.State.Pods
			cv.Nodes = oc.State.Nodes
		}
		views[i] = cv
	}

	return opts.IO.Encode(w, instrOutput.ClusterListEnvelope{Items: views})
}
