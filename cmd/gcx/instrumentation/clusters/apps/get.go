package apps

import (
	"context"
	"fmt"

	"github.com/grafana/gcx/cmd/gcx/fail"
	"github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instoutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
)

// makeGetCmd builds the "apps get <cluster> <namespace>" command.
// Calls GetAppInstrumentation, filters client-side to <namespace>.
// Emits a CLI-level not-found error when the namespace is absent.
//
// factory is called inside RunE — after cobra has parsed all flags — to
// lazily construct the appsClient.
func makeGetCmd(factory appClientFactory) *cobra.Command {
	opts := &output.Options{}
	opts.DefaultFormat("text")
	opts.RegisterCustomCodec("text", &instoutput.AppTableCodec{Wide: false})
	opts.RegisterCustomCodec("wide", &instoutput.AppTableCodec{Wide: true})
	opts.SetJSONFieldValidator(output.MakeFieldValidator(instoutput.AppView{}))

	cmd := &cobra.Command{
		Use:   "get <cluster> <namespace>",
		Short: "Get the app instrumentation entry for a single namespace",
		Long: `Get the declared Beyla instrumentation configuration for a single namespace
within the given cluster.

Reads declared state from GetAppInstrumentation and filters client-side to the
requested namespace. Exits non-zero with a not-found error when the
namespace has no declared configuration.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}

			ctx := cmd.Context()
			client, _, promHeaders, err := factory(ctx)
			if err != nil {
				return err
			}
			cluster := args[0]
			namespace := args[1]

			resp, err := client.GetAppInstrumentation(ctx, cluster)
			if err != nil {
				return err
			}

			// Cross-reference with RunK8sDiscovery to populate the discovered field.
			// Discovery RPC error short-circuits — callers can retry.
			discovered, err := client.IsNamespaceDiscovered(ctx, promHeaders, cluster, namespace)
			if err != nil {
				return fmt.Errorf("apps get: %w", err)
			}

			for _, ns := range resp.Namespaces {
				if ns.Name != namespace {
					continue
				}
				view := instoutput.AppView{
					ClusterName:     cluster,
					Name:            ns.Name,
					Autoinstrument:  ns.Autoinstrument,
					Tracing:         ns.Tracing,
					Logging:         ns.Logging,
					ProcessMetrics:  ns.ProcessMetrics,
					ExtendedMetrics: ns.ExtendedMetrics,
					Profiling:       ns.Profiling,
					Overrides:       len(ns.Apps),
					Discovered:      discovered,
				}
				// Table/wide codecs expect []AppView; JSON/YAML encode the single view directly.
				if opts.OutputFormat == "text" || opts.OutputFormat == "wide" {
					return opts.Encode(cmd.OutOrStdout(), []instoutput.AppView{view})
				}
				return opts.Encode(cmd.OutOrStdout(), view)
			}

			// Namespace is neither declared nor discovered — exit 1.
			if !discovered {
				exitCode := fail.ExitGeneralError
				return &fail.DetailedError{
					Summary: "Resource not found",
					Details: fmt.Sprintf("namespace %q not found in cluster %q", namespace, cluster),
					Suggestions: []string{
						"Run: gcx instrumentation clusters apps list " + cluster,
					},
					ExitCode: &exitCode,
				}
			}

			// Namespace is discovered but not declared — show with discovered:true, no config.
			view := instoutput.AppView{
				ClusterName: cluster,
				Name:        namespace,
				Discovered:  true,
			}
			if opts.OutputFormat == "text" || opts.OutputFormat == "wide" {
				return opts.Encode(cmd.OutOrStdout(), []instoutput.AppView{view})
			}
			return opts.Encode(cmd.OutOrStdout(), view)
		},
	}

	opts.BindFlags(cmd.Flags())
	return cmd
}

// newGetCmd is a test-facing constructor that injects a pre-built appsClient.
// Production code uses makeGetCmd(factoryFromLoader(loader)) instead.
func newGetCmd(client appsClient) *cobra.Command {
	return makeGetCmd(func(_ context.Context) (appsClient, instrumentation.BackendURLs, instrumentation.PromHeaders, error) {
		return client, instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, nil
	})
}
