package instrumentation

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/output"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type showOpts struct {
	IO output.Options
}

func (o *showOpts) setup(flags *pflag.FlagSet) {
	o.IO.DefaultFormat("yaml") // FR-045: show defaults to YAML
	o.IO.BindFlags(flags)
}

func newShowCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &showOpts{}
	cmd := &cobra.Command{
		Use:   "show <cluster>",
		Short: "Show current instrumentation config as a portable manifest.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.IO.Validate(); err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}
			ctx := cmd.Context()
			fleetClient, _, err := fleet.LoadClient(ctx, loader)
			if err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}
			client := instrum.NewClient(fleetClient)
			if err := runShow(ctx, opts, client, args[0], cmd.OutOrStdout()); err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runShow is the core show logic, separated for testability.
func runShow(ctx context.Context, opts *showOpts, client *instrum.Client, cluster string, w io.Writer) error {
	appResp, err := client.GetAppInstrumentation(ctx, cluster)
	if err != nil {
		return err
	}

	k8sResp, err := client.GetK8SInstrumentation(ctx, cluster)
	if err != nil {
		return err
	}

	cfg := instrum.InstrumentationConfig{
		APIVersion: instrum.APIVersion,
		Kind:       instrum.Kind,
		Metadata:   instrum.Metadata{Name: cluster},
		Spec:       instrum.InstrumentationSpec{},
	}

	if appResp != nil && len(appResp.Namespaces) > 0 {
		cfg.Spec.App = &instrum.AppSpec{Namespaces: appResp.Namespaces}
	}

	// Include k8s section if any field is enabled OR selection indicates monitoring is active.
	// The API may return selection=SELECTION_INCLUDED with all bools false when k8s monitoring
	// is enabled but no specific features are toggled yet.
	if k8sResp != nil && (k8sResp.CostMetrics || k8sResp.EnergyMetrics || k8sResp.ClusterEvents || k8sResp.NodeLogs || k8sResp.Selection != "") {
		cfg.Spec.K8s = &instrum.K8sSpec{
			CostMetrics:   k8sResp.CostMetrics,
			EnergyMetrics: k8sResp.EnergyMetrics,
			ClusterEvents: k8sResp.ClusterEvents,
			NodeLogs:      k8sResp.NodeLogs,
		}
	}

	return opts.IO.Encode(w, cfg)
}
