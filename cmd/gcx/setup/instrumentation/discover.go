package instrumentation

import (
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/format"
	cmdio "github.com/grafana/gcx/internal/output"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type discoverOpts struct {
	IO      cmdio.Options
	Cluster string
}

func (o *discoverOpts) setup(flags *pflag.FlagSet) {
	o.IO.RegisterCustomCodec("table", &DiscoverTableCodec{})
	o.IO.RegisterCustomCodec("wide", &DiscoverTableCodec{Wide: true})
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Cluster, "cluster", "", "Cluster name (required)")
}

func (o *discoverOpts) Validate() error {
	if o.Cluster == "" {
		return errors.New("setup/instrumentation: --cluster is required")
	}
	return o.IO.Validate()
}

func newDiscoverCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &discoverOpts{}
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover instrumentable workloads in a cluster.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			fleetClient, _, err := fleet.LoadClient(ctx, loader)
			if err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}

			client := instrum.NewClient(fleetClient)

			if err := client.SetupK8sDiscovery(ctx, opts.Cluster); err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}

			result, err := client.RunK8sDiscovery(ctx, opts.Cluster)
			if err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}

			if len(result.Namespaces) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "No workloads discovered in cluster %q\n", opts.Cluster)
				return nil
			}

			return opts.IO.Encode(cmd.OutOrStdout(), result)
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// DiscoverTableCodec renders discovered workloads as a tabular table.
type DiscoverTableCodec struct {
	Wide bool
}

// Format returns the codec's format identifier.
func (c *DiscoverTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Encode writes the discovery result as a table with columns NAMESPACE, WORKLOAD, TYPE, STATE.
// Each discovered app in each namespace gets a flattened row.
func (c *DiscoverTableCodec) Encode(w io.Writer, v any) error {
	result, ok := v.(*instrum.RunK8sDiscoveryResponse)
	if !ok {
		return errors.New("invalid data type for table codec: expected *RunK8sDiscoveryResponse")
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tWORKLOAD\tTYPE\tSTATE")

	for _, ns := range result.Namespaces {
		for _, app := range ns.Apps {
			appType := app.Type
			if appType == "" {
				appType = "-"
			}
			state := app.State
			if state == "" {
				state = "-"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", ns.Name, app.Name, appType, state)
		}
	}

	return tw.Flush()
}

// Decode is not supported for table format.
func (c *DiscoverTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}
