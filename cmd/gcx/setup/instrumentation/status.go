package instrumentation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	internalconfig "github.com/grafana/gcx/internal/config"
	fleetbase "github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	queryprom "github.com/grafana/gcx/internal/query/prometheus"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ClusterStatus is the merged per-cluster view combining instrumentation state
// and Beyla error counts.
type ClusterStatus struct {
	Name        string  `json:"name" yaml:"name"`
	State       string  `json:"state" yaml:"state"`
	BeylaErrors float64 `json:"beylaErrors" yaml:"beylaErrors"`
}

type statusOpts struct {
	IO      cmdio.Options
	Cluster string
}

func (o *statusOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &StatusTableCodec{})
	o.IO.RegisterCustomCodec("wide", &StatusTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Cluster, "cluster", "", "Filter by cluster name")
}

// newStatusCommand creates the instrumentation status subcommand.
func newStatusCommand(loader fleetbase.ConfigLoader) *cobra.Command {
	opts := &statusOpts{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show instrumentation status across clusters.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.IO.Validate(); err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}

			ctx := cmd.Context()

			base, _, err := fleetbase.LoadClient(ctx, loader)
			if err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}
			client := instrum.NewClient(base)

			monResp, err := client.RunK8sMonitoring(ctx)
			if err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}

			// Best-effort Beyla error query — skip if Grafana config unavailable.
			beylaErrors := queryBeylaErrors(ctx, loader)

			statuses := make([]ClusterStatus, 0, len(monResp.Clusters))
			for _, cs := range monResp.Clusters {
				if opts.Cluster != "" && cs.Name != opts.Cluster {
					continue
				}
				statuses = append(statuses, ClusterStatus{
					Name:        cs.Name,
					State:       cs.State,
					BeylaErrors: beylaErrors[cs.Name],
				})
			}

			return opts.IO.Encode(cmd.OutOrStdout(), statuses)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// grafanaConfigLoader is the extended interface needed to run the Beyla error
// Prometheus query. *providers.ConfigLoader satisfies this interface.
type grafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
	LoadFullConfig(ctx context.Context) (*internalconfig.Config, error)
}

// queryBeylaErrors runs the Prometheus query for Beyla error counts against
// the stack's Mimir datasource. Returns a cluster→errors map, or nil on any
// failure (the caller treats nil as zero errors for all clusters).
func queryBeylaErrors(ctx context.Context, loader fleetbase.ConfigLoader) map[string]float64 {
	gl, ok := loader.(grafanaConfigLoader)
	if !ok {
		return nil
	}

	restCfg, err := gl.LoadGrafanaConfig(ctx)
	if err != nil {
		return nil
	}

	fullCfg, err := gl.LoadFullConfig(ctx)
	if err != nil {
		return nil
	}
	curCtx := fullCfg.GetCurrentContext()
	if curCtx == nil {
		return nil
	}
	dsUID := internalconfig.DefaultDatasourceUID(*curCtx, "prometheus")
	if dsUID == "" {
		return nil
	}

	promClient, err := queryprom.NewClient(restCfg)
	if err != nil {
		return nil
	}

	const query = `sum by (k8s_cluster_name) (increase(beyla_instrumentation_errors_total[1h]))`
	resp, err := promClient.Query(ctx, dsUID, queryprom.QueryRequest{Query: query})
	if err != nil {
		return nil
	}

	result := make(map[string]float64)
	for _, sample := range resp.Data.Result {
		clusterName := sample.Metric["k8s_cluster_name"]
		if clusterName == "" || len(sample.Value) < 2 {
			continue
		}
		valStr, ok := sample.Value[1].(string)
		if !ok {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		result[clusterName] = val
	}
	return result
}

// StatusTableCodec renders []ClusterStatus as a tab-separated table.
type StatusTableCodec struct {
	Wide bool
}

// Format returns the codec's format identifier.
func (c *StatusTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes the cluster status list as a table.
func (c *StatusTableCodec) Encode(w io.Writer, v any) error {
	statuses, ok := v.([]ClusterStatus)
	if !ok {
		return errors.New("invalid data type for table codec: expected []ClusterStatus")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CLUSTER\tSTATE\tBEYLA ERRORS")
	for _, s := range statuses {
		fmt.Fprintf(tw, "%s\t%s\t%.0f\n", s.Name, s.State, s.BeylaErrors)
	}
	return tw.Flush()
}

// Decode is not supported for table format.
func (c *StatusTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
