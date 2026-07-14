package clusters

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instrOutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &instrOutput.ClusterTableCodec{Wide: false})
	o.IO.RegisterCustomCodec("wide", &instrOutput.ClusterTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(instrOutput.ClusterView{}))
	o.IO.BindFlags(flags)
}

func (o *getOpts) Validate() error {
	return o.IO.Validate()
}

func newGetCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &getOpts{}
	cmd := &cobra.Command{
		Use:   "get <cluster>",
		Short: "Show declared config and observed status for a cluster",
		Long: `Show the declared K8s monitoring configuration and observed instrumentation
status for a single cluster.

The declared configuration is fetched via GetK8SInstrumentation. The observed
status is cross-referenced with RunK8sMonitoring. If the cluster is absent from
RunK8sMonitoring, ListPipelines is checked: a K8s monitoring pipeline present
means PENDING_INSTRUMENTATION; absent means NOT_INSTRUMENTED.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Get cluster "prod-eu" in table format
  gcx instrumentation clusters get prod-eu

  # Get in JSON format
  gcx instrumentation clusters get prod-eu -o json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			clusterName := args[0]

			base, _, err := fleet.LoadClient(ctx, loader)
			if err != nil {
				return fmt.Errorf("clusters get: %w", err)
			}
			client := instrumentation.NewClient(base)

			return runGet(ctx, opts, client, clusterName, cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runGet implements the core get logic. Separated from newGetCommand for
// testability with fake clients.
func runGet(
	ctx context.Context,
	opts *getOpts,
	client clusterClient,
	clusterName string,
	w io.Writer,
) error {
	// Fetch declared configuration.
	resp, err := client.GetK8SInstrumentation(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("clusters get: %w", err)
	}
	cl := resp.Cluster

	// Backend returns HTTP 200 with zero-valued proto for unknown clusters.
	// Detect client-side via the empty-default shape and exit 1 (ExitGeneralError)
	// for not-found — exit 4 is reserved for ExitPartialFailure per
	// docs/design/exit-codes.md.
	if instrumentation.IsEmptyDefaultCluster(cl) {
		exitCode := gcxerrors.ExitGeneralError
		return &gcxerrors.DetailedError{
			Summary: "Resource not found",
			Details: fmt.Sprintf("cluster %q has no K8s monitoring configuration", clusterName),
			Suggestions: []string{
				"Run: gcx instrumentation clusters list",
			},
			ExitCode: &exitCode,
		}
	}

	// Cross-reference with observed state from RunK8sMonitoring.
	monResp, err := client.RunK8sMonitoring(ctx)
	if err != nil {
		return fmt.Errorf("clusters get: RunK8sMonitoring: %w", err)
	}

	cv := instrOutput.ClusterView{
		Name:          cl.Name,
		Selection:     cl.Selection,
		CostMetrics:   cl.CostMetrics,
		EnergyMetrics: cl.EnergyMetrics,
		ClusterEvents: cl.ClusterEvents,
		NodeLogs:      cl.NodeLogs,
	}

	// Merge observed state if the cluster is visible to RunK8sMonitoring.
	for _, state := range monResp.Clusters {
		if state.Name == clusterName {
			cv.InstrumentationStatus = state.InstrumentationStatus
			cv.Namespaces = len(state.Namespaces)
			cv.Workloads = state.Workloads
			cv.Pods = state.Pods
			cv.Nodes = state.Nodes
			break
		}
	}

	// If not visible in RunK8sMonitoring, fall back to ListPipelines.
	if cv.InstrumentationStatus == "" {
		pipelines, err := client.ListPipelines(ctx)
		if err != nil {
			return fmt.Errorf("clusters get: ListPipelines: %w", err)
		}
		cv.InstrumentationStatus = pipelineFallbackStatus(clusterName, pipelines)
	}

	// For table/wide formats, the codec expects []ClusterView; for JSON/YAML
	// encode the single view directly.
	if opts.IO.OutputFormat == "table" || opts.IO.OutputFormat == "wide" {
		return opts.IO.Encode(w, []instrOutput.ClusterView{cv})
	}
	return opts.IO.Encode(w, cv)
}
