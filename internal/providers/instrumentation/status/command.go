package status

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	internalconfig "github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	queryprom "github.com/grafana/gcx/internal/query/prometheus"
	"github.com/grafana/gcx/internal/style"
	"github.com/grafana/promql-builder/go/promql"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// ClusterStatus is the merged per-cluster view combining instrumentation state,
// K8s monitoring flags, and Beyla error counts.
type ClusterStatus struct {
	Name          string  `json:"name" yaml:"name"`
	State         string  `json:"state" yaml:"state"`
	Workloads     int     `json:"workloads" yaml:"workloads"`
	Pods          int     `json:"pods" yaml:"pods"`
	Nodes         int     `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	Namespaces    int     `json:"namespaces,omitempty" yaml:"namespaces,omitempty"`
	BeylaErrors   float64 `json:"beylaErrors" yaml:"beylaErrors"`
	CostMetrics   bool    `json:"costMetrics" yaml:"costMetrics"`
	ClusterEvents bool    `json:"clusterEvents" yaml:"clusterEvents"`
	EnergyMetrics bool    `json:"energyMetrics" yaml:"energyMetrics"`
	NodeLogs      bool    `json:"nodeLogs" yaml:"nodeLogs"`
}

type statusOpts struct {
	IO      cmdio.Options
	Cluster string
}

func (o *statusOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Cluster, "cluster", "", "Filter by cluster name")
}

// Validate checks that IO options are valid.
func (o *statusOpts) Validate() error {
	return o.IO.Validate()
}

// grafanaConfigLoader is the extended interface needed to run the Beyla error
// Prometheus query. *providers.ConfigLoader satisfies this interface.
type grafanaConfigLoader interface {
	LoadGrafanaConfig(ctx context.Context) (internalconfig.NamespacedRESTConfig, error)
	LoadFullConfig(ctx context.Context) (*internalconfig.Config, error)
}

func newCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &statusOpts{}
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show instrumentation status across clusters.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			ctx := cmd.Context()

			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}
			client := instrumentation.NewClient(r.Client)
			promHdrs := instrumentation.PromHeadersFromStack(r.Stack)

			monResp, err := client.RunK8sMonitoring(ctx, promHdrs)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			beylaErrors := queryBeylaErrors(ctx, loader)

			// Fan out GetK8SInstrumentation per cluster to populate monitoring flags.
			type clusterK8sFlags struct {
				costMetrics   bool
				clusterEvents bool
				energyMetrics bool
				nodeLogs      bool
			}
			var footer instrumentation.Footer

			k8sFlags := make([]clusterK8sFlags, len(monResp.Clusters))
			g, gctx := errgroup.WithContext(ctx)
			g.SetLimit(10)
			for i, cs := range monResp.Clusters {
				if opts.Cluster != "" && cs.Name != opts.Cluster {
					continue
				}
				name := cs.Name
				idx := i
				g.Go(func() error {
					resp, err := client.GetK8SInstrumentation(gctx, name)
					if err != nil {
						footer.Warnf("failed to fetch K8s monitoring flags for cluster %s: %v", name, err)
						return nil // warning recorded, continue — non-fatal: unavailable cluster shows flags as false
					}
					k8sFlags[idx] = clusterK8sFlags{
						costMetrics:   resp.CostMetrics,
						clusterEvents: resp.ClusterEvents,
						energyMetrics: resp.EnergyMetrics,
						nodeLogs:      resp.NodeLogs,
					}
					return nil
				})
			}
			_ = g.Wait()

			statuses := make([]ClusterStatus, 0, len(monResp.Clusters))
			for i, cs := range monResp.Clusters {
				if opts.Cluster != "" && cs.Name != opts.Cluster {
					continue
				}
				statuses = append(statuses, ClusterStatus{
					Name:          cs.Name,
					State:         cs.InstrumentationStatus,
					Workloads:     cs.Workloads,
					Pods:          cs.Pods,
					Nodes:         cs.Nodes,
					Namespaces:    len(cs.Namespaces),
					BeylaErrors:   beylaErrors[cs.Name],
					CostMetrics:   k8sFlags[i].costMetrics,
					ClusterEvents: k8sFlags[i].clusterEvents,
					EnergyMetrics: k8sFlags[i].energyMetrics,
					NodeLogs:      k8sFlags[i].nodeLogs,
				})
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), statuses); err != nil {
				return err
			}
			if len(statuses) == 0 {
				footer.Notef("no clusters found.")
			}
			footer.Notef("data reflects Alloy collector-reported state (refresh ~30s).")
			if len(statuses) == 0 {
				footer.Hint("to register a cluster", "gcx instrumentation clusters setup --help")
			}
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// Command returns the status cobra command.
func Command(loader *providers.ConfigLoader) *cobra.Command {
	return newCommand(loader)
}

// queryBeylaErrors runs the Prometheus query for Beyla error counts against
// the stack's Mimir datasource. Returns a cluster→errors map, or nil on any
// failure (the caller treats nil as zero errors for all clusters).
func queryBeylaErrors(ctx context.Context, loader fleet.ConfigLoader) map[string]float64 {
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

	// sum by (k8s_cluster_name) (increase(beyla_instrumentation_errors_total[1h]))
	queryExpr, err := promql.Sum(
		promql.Increase(
			promql.NewVectorExprBuilder().Metric("beyla_instrumentation_errors_total").Range("1h"),
		),
	).By([]string{"k8s_cluster_name"}).Build()
	if err != nil {
		return nil
	}
	resp, err := promClient.Query(ctx, dsUID, queryprom.QueryRequest{Query: queryExpr.String()})
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

// TableCodec renders []ClusterStatus as a tab-separated table.
type TableCodec struct {
	Wide bool
}

// Format returns the codec's format identifier.
func (c *TableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes the cluster status list as a table.
func (c *TableCodec) Encode(w io.Writer, v any) error {
	statuses, ok := v.([]ClusterStatus)
	if !ok {
		return errors.New("invalid data type for table codec: expected []ClusterStatus")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("CLUSTER", "STATUS", "NODES", "WORKLOADS", "PODS", "NAMESPACES", "K8S FLAGS", "BEYLA ERRORS")
		for _, s := range statuses {
			t.Row(s.Name, s.State, strconv.Itoa(s.Nodes), strconv.Itoa(s.Workloads), strconv.Itoa(s.Pods), strconv.Itoa(s.Namespaces), k8sFlagsStr(s), fmt.Sprintf("%.0f", s.BeylaErrors))
		}
	} else {
		t = style.NewTable("CLUSTER", "STATUS", "WORKLOADS", "PODS", "K8S FLAGS", "BEYLA ERRORS")
		for _, s := range statuses {
			t.Row(s.Name, s.State, strconv.Itoa(s.Workloads), strconv.Itoa(s.Pods), k8sFlagsStr(s), fmt.Sprintf("%.0f", s.BeylaErrors))
		}
	}
	return t.Render(w)
}

// Decode is not supported for table format.
func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// k8sFlagsStr returns a compact representation of the K8s monitoring flags.
// Enabled flags are shown by abbreviation; all disabled shows "none".
func k8sFlagsStr(s ClusterStatus) string {
	var flags []string
	if s.CostMetrics {
		flags = append(flags, "cost")
	}
	if s.ClusterEvents {
		flags = append(flags, "events")
	}
	if s.EnergyMetrics {
		flags = append(flags, "energy")
	}
	if s.NodeLogs {
		flags = append(flags, "nodelogs")
	}
	if len(flags) == 0 {
		return "none"
	}
	return strings.Join(flags, ",")
}
