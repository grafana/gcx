package instrumentation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/fleet"
	instrum "github.com/grafana/gcx/internal/setup/instrumentation"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

type applyOpts struct {
	File   string
	DryRun bool
}

func (o *applyOpts) setup(flags *pflag.FlagSet) {
	flags.StringVarP(&o.File, "filename", "f", "", "Path to the InstrumentationConfig manifest file (required)")
	flags.BoolVar(&o.DryRun, "dry-run", false, "Preview changes without applying")
}

func (o *applyOpts) Validate() error {
	if o.File == "" {
		return errors.New("setup/instrumentation: -f/--filename is required")
	}
	return nil
}

func newApplyCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &applyOpts{}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply an InstrumentationConfig manifest.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()
			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("setup/instrumentation: %w", err)
			}
			client := instrum.NewClient(r.Client)
			urls := instrum.BackendURLsFromStack(r.Stack)
			return runApply(ctx, opts, client, urls, r.Stack, cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runApply is the core apply logic, separated for testability.
func runApply(ctx context.Context, opts *applyOpts, client *instrum.Client, urls instrum.BackendURLs, stack cloud.StackInfo, out io.Writer) error {
	data, err := os.ReadFile(opts.File)
	if err != nil {
		return fmt.Errorf("setup/instrumentation: %w", err)
	}

	var config instrum.InstrumentationConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("setup/instrumentation: invalid manifest: %w", err)
	}

	if err := config.Validate(); err != nil {
		return fmt.Errorf("setup/instrumentation: %w", err)
	}

	cluster := config.Metadata.Name

	if config.Spec.App == nil && config.Spec.K8s == nil {
		fmt.Fprintf(out, "nothing to apply: manifest has no spec.app or spec.k8s sections\n")
		return nil
	}

	if config.Spec.App != nil {
		if err := applyApp(ctx, opts, client, cluster, config.Spec.App, urls, out); err != nil {
			return err
		}
	}

	if config.Spec.K8s != nil {
		if err := applyK8s(ctx, opts, client, cluster, config.Spec.K8s, urls, out); err != nil {
			return err
		}
	}

	if !opts.DryRun {
		printPostApplyHint(out, cluster, stack)
	}

	return nil
}

// printPostApplyHint prints guidance on how to connect the cluster to Grafana Cloud
// via the grafana-cloud-onboarding Helm chart. This surfaces the fleet management URL
// and auth credentials that are otherwise not discoverable from gcx output.
func printPostApplyHint(w io.Writer, cluster string, stack cloud.StackInfo) {
	fleetURL := stack.AgentManagementInstanceURL
	if fleetURL == "" {
		return
	}
	instanceID := stack.AgentManagementInstanceID
	fmt.Fprintf(w, "\nTo connect cluster %q to Grafana Cloud, install the Helm chart:\n\n", cluster)
	fmt.Fprintf(w, "  helm repo add grafana https://grafana.github.io/helm-charts\n")
	fmt.Fprintf(w, "  helm upgrade --install grafana-cloud -n monitoring --create-namespace \\\n")
	fmt.Fprintf(w, "    grafana/grafana-cloud-onboarding \\\n")
	fmt.Fprintf(w, "    --set \"cluster.name=%s\" \\\n", cluster)
	fmt.Fprintf(w, "    --set \"grafanaCloud.fleetManagement.url=%s\" \\\n", fleetURL)
	fmt.Fprintf(w, "    --set \"grafanaCloud.fleetManagement.auth.username=%d\" \\\n", instanceID)
	fmt.Fprintf(w, "    --set \"grafanaCloud.fleetManagement.auth.password=<YOUR_CLOUD_ACCESS_TOKEN>\" \\\n")
	fmt.Fprintf(w, "    --wait\n\n")
	fmt.Fprintf(w, "Then verify: gcx setup instrumentation status\n")
}

func applyApp(ctx context.Context, opts *applyOpts, client *instrum.Client, cluster string, app *instrum.AppSpec, urls instrum.BackendURLs, out io.Writer) error {
	remoteResp, err := client.GetAppInstrumentation(ctx, cluster)
	if err != nil {
		return fmt.Errorf("setup/instrumentation: %w", err)
	}

	remoteApp := &instrum.AppSpec{Namespaces: remoteResp.Namespaces}
	diff := instrum.Compare(app, remoteApp)
	if !diff.IsEmpty() && !opts.DryRun {
		return buildOptimisticLockError(cluster, diff)
	}

	if opts.DryRun {
		if !diff.IsEmpty() {
			fmt.Fprintf(out, "dry-run: remote has items not in local manifest (would fail without --dry-run):\n")
			writeDiffSummary(out, diff)
		}
		fmt.Fprintf(out, "dry-run: would apply spec.app for cluster %q (%d namespace(s))\n", cluster, len(app.Namespaces))
		return nil
	}

	if err := client.SetAppInstrumentation(ctx, cluster, app.Namespaces, urls); err != nil {
		return fmt.Errorf("setup/instrumentation: %w", err)
	}
	fmt.Fprintf(out, "applied spec.app for cluster %q\n", cluster)
	return nil
}

func applyK8s(ctx context.Context, opts *applyOpts, client *instrum.Client, cluster string, k8s *instrum.K8sSpec, urls instrum.BackendURLs, out io.Writer) error {
	remoteResp, err := client.GetK8SInstrumentation(ctx, cluster)
	if err != nil {
		return fmt.Errorf("setup/instrumentation: %w", err)
	}

	// Optimistic lock for k8s: fail if remote has features enabled that the local manifest doesn't.
	remoteExtra := k8sRemoteOnlyFeatures(k8s, remoteResp)
	if len(remoteExtra) > 0 && !opts.DryRun {
		return buildK8sOptimisticLockError(cluster, remoteExtra)
	}

	if opts.DryRun {
		if len(remoteExtra) > 0 {
			fmt.Fprintf(out, "dry-run: remote k8s config has features not in local manifest (would fail without --dry-run): %s\n",
				strings.Join(remoteExtra, ", "))
		}
		fmt.Fprintf(out, "dry-run: would apply spec.k8s for cluster %q\n", cluster)
		return nil
	}

	if err := client.SetK8SInstrumentation(ctx, cluster, *k8s, urls); err != nil {
		return fmt.Errorf("setup/instrumentation: %w", err)
	}
	fmt.Fprintf(out, "applied spec.k8s for cluster %q\n", cluster)
	return nil
}

// k8sRemoteOnlyFeatures returns feature names that are enabled remotely but not in the local manifest.
func k8sRemoteOnlyFeatures(local *instrum.K8sSpec, remote *instrum.GetK8SInstrumentationResponse) []string {
	if remote == nil {
		return nil
	}
	var extra []string
	if remote.CostMetrics && !local.CostMetrics {
		extra = append(extra, "costmetrics")
	}
	if remote.EnergyMetrics && !local.EnergyMetrics {
		extra = append(extra, "energymetrics")
	}
	if remote.ClusterEvents && !local.ClusterEvents {
		extra = append(extra, "clusterevents")
	}
	if remote.NodeLogs && !local.NodeLogs {
		extra = append(extra, "nodelogs")
	}
	return extra
}

func writeDiffSummary(w io.Writer, diff *instrum.Diff) {
	for _, ns := range diff.Namespaces {
		fmt.Fprintf(w, "  - namespace %q\n", ns.Namespace)
	}
	for _, app := range diff.Apps {
		fmt.Fprintf(w, "  - app %q in namespace %q\n", app.App, app.Namespace)
	}
}

func buildK8sOptimisticLockError(cluster string, remoteExtra []string) error {
	var sb strings.Builder
	sb.WriteString("setup/instrumentation: remote k8s config has features not in local manifest: ")
	sb.WriteString(strings.Join(remoteExtra, ", "))
	fmt.Fprintf(&sb, "\nuse 'gcx setup instrumentation show %s -o yaml' to see the current remote config and reconcile", cluster)
	return fmt.Errorf("%s", sb.String())
}

func buildOptimisticLockError(cluster string, diff *instrum.Diff) error {
	var sb strings.Builder
	sb.WriteString("setup/instrumentation: remote config has items not in local manifest:\n")
	for _, ns := range diff.Namespaces {
		fmt.Fprintf(&sb, "  - namespace %q (not in local manifest)\n", ns.Namespace)
	}
	for _, app := range diff.Apps {
		fmt.Fprintf(&sb, "  - app %q in namespace %q (not in local manifest)\n", app.App, app.Namespace)
	}
	fmt.Fprintf(&sb, "Use 'gcx setup instrumentation show %s -o yaml' to see the current remote config and reconcile.", cluster)
	return fmt.Errorf("%s", sb.String())
}
