package discover

import (
	"errors"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/shared"
	"github.com/grafana/gcx/internal/style"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type discoverOpts struct {
	IO cmdio.Options
}

func (o *discoverOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &TableCodec{})
	o.IO.RegisterCustomCodec("wide", &TableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
}

// Validate checks that IO options are valid.
func (o *discoverOpts) Validate() error {
	return o.IO.Validate()
}

func newCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &discoverOpts{}
	cmd := &cobra.Command{
		Use:   "discover <cluster>",
		Short: "Discover instrumentable workloads in a cluster.",
		Long: `List workloads reported by the Alloy collector installed in <cluster>.

Results reflect Alloy collector-reported state — workloads appear only after
Alloy is installed and reporting to Grafana Cloud (refresh ~30s). An empty
result does not imply an empty cluster; it means Alloy has not yet reported
any workloads from that cluster.

For direct cluster inspection (e.g., before Alloy is installed), use
kubectl against your kubeconfig:

  kubectl get pods --all-namespaces`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			clusterName := args[0]
			ctx := cmd.Context()

			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			client := instrumentation.NewClient(r.Client)
			urls := instrumentation.BackendURLsFromStack(r.Stack)
			promHdrs := instrumentation.PromHeadersFromStack(r.Stack)

			if err := client.SetupK8sDiscovery(ctx, urls, promHdrs); err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			result, err := client.RunK8sDiscovery(ctx, promHdrs)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			filtered := &instrumentation.RunK8sDiscoveryResponse{}
			for _, item := range result.Items {
				if item.ClusterName == clusterName {
					filtered.Items = append(filtered.Items, item)
				}
			}

			if err := opts.IO.Encode(cmd.OutOrStdout(), filtered); err != nil {
				return err
			}
			var footer instrumentation.Footer
			if len(filtered.Items) == 0 {
				footer.Notef("no workloads found in cluster %q.", clusterName)
			}
			footer.Notef("data reflects Alloy collector-reported state — workloads appear only after Alloy is installed and reporting (refresh ~30s).")
			footer.Hint("to inspect the cluster directly", "kubectl get pods --all-namespaces")
			footer.Print(cmd.ErrOrStderr())
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// Command returns the discover cobra command.
func Command(loader *providers.ConfigLoader) *cobra.Command {
	return newCommand(loader)
}

// TableCodec renders discovered workloads as a tabular table.
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

// Encode writes the discovery result as a table.
func (c *TableCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(*instrumentation.RunK8sDiscoveryResponse)
	if !ok {
		return errors.New("invalid data type for table codec: expected *RunK8sDiscoveryResponse")
	}

	var t *style.TableBuilder
	if c.Wide {
		t = style.NewTable("NAMESPACE", "WORKLOAD", "TYPE", "STATUS", "LANG", "OS")
		for _, item := range result.Items {
			t.Row(item.Namespace, item.DisplayName,
				shared.ValOrDash(item.WorkloadType), shared.ValOrDash(item.InstrumentationStatus),
				shared.ValOrDash(item.Lang), shared.ValOrDash(item.OS))
		}
	} else {
		t = style.NewTable("NAMESPACE", "WORKLOAD", "TYPE", "STATUS")
		for _, item := range result.Items {
			t.Row(item.Namespace, item.DisplayName,
				shared.ValOrDash(item.WorkloadType), shared.ValOrDash(item.InstrumentationStatus))
		}
	}

	return t.Render(w)
}

// Decode is not supported for table format.
func (c *TableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
