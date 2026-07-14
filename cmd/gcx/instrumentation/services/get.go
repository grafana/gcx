package services

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/gcxerrors"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instrumout "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type getOpts struct {
	IO cmdio.Options
}

func (o *getOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("text")
	o.IO.RegisterCustomCodec("text", &instrumout.ServiceTableCodec{Wide: false})
	o.IO.RegisterCustomCodec("wide", &instrumout.ServiceTableCodec{Wide: true})
	o.IO.SetJSONFieldValidator(cmdio.MakeFieldValidator(instrumout.ServiceView{}))
	o.IO.BindFlags(flags)
}

func (o *getOpts) Validate() error {
	return o.IO.Validate()
}

func newGetCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &getOpts{}

	cmd := &cobra.Command{
		Use:   "get <cluster> <namespace> <service>",
		Short: "Get a single discovered workload by (cluster, namespace, service)",
		Long: `Get a single workload discovered by the Beyla survey collector.

Calls RunK8sDiscovery() (fleet-wide RPC) and filters to the requested workload.
Returns an error if the workload is not found.

Examples:
  gcx instrumentation services get prod-1 checkout frontend
  gcx instrumentation services get prod-1 checkout frontend -o json

Workload-level Selection / override state is not surfaced on this command. To inspect a workload's override, run "gcx instrumentation clusters apps list --cluster=<cluster> --namespace=<namespace>".`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			base, _, err := fleet.LoadClient(ctx, loader)
			if err != nil {
				return fmt.Errorf("services get: %w", err)
			}
			client := instrumentation.NewClient(base)
			return runGet(ctx, &opts.IO, client, args[0], args[1], args[2], cmd.OutOrStdout())
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// runGet performs the RunK8sDiscovery call and filters to the requested workload.
// Returns an error if the workload is not found.
// Separated from RunE for testability.
func runGet(
	ctx context.Context,
	outOpts *cmdio.Options,
	client *instrumentation.Client,
	cluster, namespace, service string,
	out io.Writer,
) error {
	resp, err := client.RunK8sDiscovery(ctx)
	if err != nil {
		return fmt.Errorf("services get: %w", err)
	}

	for _, item := range resp.Items {
		if item.ClusterName == cluster && item.Namespace == namespace && item.Name == service {
			view := instrumout.ServiceView{
				ClusterName:           item.ClusterName,
				Namespace:             item.Namespace,
				Name:                  item.Name,
				WorkloadType:          item.WorkloadType,
				DisplayNamespace:      item.DisplayNamespace,
				DisplayName:           item.DisplayName,
				OS:                    item.OS,
				Lang:                  item.Lang,
				InstrumentationStatus: item.InstrumentationStatus,
			}
			// For table/text/wide: wrap in a slice so the codec can render a single row.
			// For JSON/YAML: encode the single object directly (mirrors clusters/get.go pattern).
			if outOpts.OutputFormat == "text" || outOpts.OutputFormat == "wide" {
				return outOpts.Encode(out, []instrumout.ServiceView{view})
			}
			return outOpts.Encode(out, view)
		}
	}

	exitCode := gcxerrors.ExitGeneralError
	return &gcxerrors.DetailedError{
		Summary: "Resource not found",
		Details: fmt.Sprintf("workload %q not found in namespace %q (cluster %q)", service, namespace, cluster),
		Suggestions: []string{
			fmt.Sprintf("Run: gcx instrumentation services list --cluster=%s --namespace=%s", cluster, namespace),
		},
		ExitCode: &exitCode,
	}
}
