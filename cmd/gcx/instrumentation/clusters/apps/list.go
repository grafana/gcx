package apps

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
)

// makeListCmd builds the "apps list <cluster>" command.
// Calls GetAppInstrumentation (single RPC) and returns all namespace
// entries via the AppTableCodec.
//
// factory is called inside RunE — after cobra has parsed all flags — to
// lazily construct the appsClient.
func makeListCmd(factory appClientFactory) *cobra.Command {
	opts := &output.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &instoutput.AppTableCodec{Wide: false})
	opts.RegisterCustomCodec("wide", &instoutput.AppTableCodec{Wide: true})
	opts.SetJSONFieldValidator(output.MakeFieldValidator(instoutput.AppView{}))

	cmd := &cobra.Command{
		Use:   "list <cluster>",
		Short: "List all namespace app instrumentation entries for a cluster",
		Long: `List all namespace-level Beyla instrumentation entries for the given cluster.

Reads declared state from GetAppInstrumentation (a single RPC call). The output
reflects the configuration stored in Grafana Cloud, not the live observed state.
Use "gcx instrumentation status" for observed-state status.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			// --json list (field discovery): introspect AppView shape without
			// requiring a cluster positional or making any API call.
			if opts.JSONDiscovery {
				return opts.Encode(cmd.OutOrStdout(), instoutput.AppListEnvelope{Items: []instoutput.AppView{{}}})
			}

			if len(args) != 1 {
				return fmt.Errorf("accepts 1 arg(s), received %d", len(args))
			}

			ctx := cmd.Context()
			client, _, err := factory(ctx)
			if err != nil {
				return err
			}
			cluster := args[0]

			resp, err := client.GetAppInstrumentation(ctx, cluster)
			if err != nil {
				return err
			}

			// Call RunK8sDiscovery once and build a namespace set for this cluster.
			// This populates the discovered field on each AppView.
			discResp, err := client.RunK8sDiscovery(ctx)
			if err != nil {
				return err
			}
			discoveredNS := make(map[string]bool, len(discResp.Items))
			for _, item := range discResp.Items {
				if item.ClusterName == cluster {
					discoveredNS[item.Namespace] = true
				}
			}

			views := make([]instoutput.AppView, 0, len(resp.Namespaces))
			for _, ns := range resp.Namespaces {
				views = append(views, instoutput.AppView{
					ClusterName:     cluster,
					Name:            ns.Name,
					Discovered:      discoveredNS[ns.Name],
					Autoinstrument:  ns.Autoinstrument,
					Tracing:         ns.Tracing,
					Logging:         ns.Logging,
					ProcessMetrics:  ns.ProcessMetrics,
					ExtendedMetrics: ns.ExtendedMetrics,
					Profiling:       ns.Profiling,
					Overrides:       len(ns.Apps),
				})
			}

			return opts.Encode(cmd.OutOrStdout(), instoutput.AppListEnvelope{Items: views})
		},
	}

	opts.BindFlags(cmd.Flags())
	return cmd
}

// newListCmd is a test-facing constructor that injects a pre-built appsClient.
// Production code uses makeListCmd(factoryFromLoader(loader)) instead.
func newListCmd(client appsClient) *cobra.Command {
	return makeListCmd(func(_ context.Context) (appsClient, instrumentation.BackendURLs, error) {
		return client, instrumentation.BackendURLs{}, nil
	})
}
