package status

import (
	"fmt"

	fleetbase "github.com/grafana/gcx/internal/fleet"
	cmdio "github.com/grafana/gcx/internal/output"
	instrum "github.com/grafana/gcx/internal/providers/instrumentation"
	instroutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type statusOpts struct {
	IO        cmdio.Options
	Cluster   string
	Namespace string
	loader    fleetbase.ConfigLoader
}

func (o *statusOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("table")
	o.IO.BindFlags(flags)
	flags.StringVar(&o.Cluster, "cluster", "", "Filter output to a specific cluster")
	flags.StringVar(&o.Namespace, "namespace", "", "Filter to a specific namespace; switches to workload-level view")
}

// Validate registers the mode-dependent codecs (driven by --namespace) and
// validates the resolved IO options. Output codecs depend on --namespace, so
// registration must happen here — after flag parsing but before IO.Validate.
func (o *statusOpts) Validate() error {
	if o.Namespace != "" {
		o.IO.RegisterCustomCodec("table", &instroutput.ServiceTableCodec{})
		o.IO.RegisterCustomCodec("wide", &instroutput.ServiceTableCodec{Wide: true})
		o.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(instroutput.ServiceView{}))
	} else {
		o.IO.RegisterCustomCodec("table", &instroutput.ClusterTableCodec{})
		o.IO.RegisterCustomCodec("wide", &instroutput.ClusterTableCodec{Wide: true})
		o.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(instroutput.ClusterView{}))
	}
	return o.IO.Validate()
}

// Command returns the "gcx instrumentation status" cobra command.
//
// Without flags, lists all clusters — including pre-Alloy clusters that have
// been configured but whose Alloy collector has not yet started reporting —
// with their observed InstrumentationStatus.
//
// --cluster narrows the result to a single cluster.
// --namespace additionally calls RunK8sDiscovery and switches the output to a
// workload-level (service) view for the given namespace.
//
// loader is passed from the instrumentation umbrella command, which binds
// --context and --config on its PersistentFlags.
func Command(loader fleetbase.ConfigLoader) *cobra.Command {
	opts := &statusOpts{loader: loader}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show observed instrumentation state for clusters and namespaces.",
		Long: `Show observed instrumentation state across all clusters, or narrow to a
specific cluster or namespace.

Without flags, lists all clusters including pre-Alloy clusters that have been
configured but whose Alloy collector has not yet started reporting (shown as
PENDING_INSTRUMENTATION).

Use --cluster to filter to a single cluster. Add --namespace to drill down to
workload-level status for a specific namespace, powered by RunK8sDiscovery.`,

		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return fmt.Errorf("instrumentation status: %w", err)
			}

			ctx := cmd.Context()

			base, _, err := fleetbase.LoadClient(ctx, opts.loader)
			if err != nil {
				return fmt.Errorf("instrumentation status: %w", err)
			}

			client := instrum.NewClient(base)

			mon := &monitoringAdapter{client: client}
			// *instrum.Client satisfies pipelineSource directly (ListPipelines matches).
			var pipe pipelineSource = client
			disc := &discoveryAdapter{client: client}

			result, err := run(ctx, opts.Cluster, opts.Namespace, mon, pipe, disc)
			if err != nil {
				return fmt.Errorf("instrumentation status: %w", err)
			}

			// Wrap the result in the canonical list envelope for JSON output
			// (docs/design/output.md §101: list endpoints emit {"items":[...]}).
			// Table/wide codecs unwrap the envelope at the codec boundary.
			var encoded any
			switch v := result.(type) {
			case []instroutput.ClusterView:
				encoded = instroutput.ClusterListEnvelope{Items: v}
			case []instroutput.ServiceView:
				encoded = instroutput.ServiceListEnvelope{Items: v}
			default:
				encoded = result
			}

			return opts.IO.Encode(cmd.OutOrStdout(), encoded)
		},
	}

	opts.setup(cmd.Flags())

	return cmd
}
