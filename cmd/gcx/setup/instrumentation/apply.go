package instrumentation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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
			return runApply(ctx, opts, client, urls, cmd.OutOrStdout())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runApply is the core apply logic, separated for testability.
func runApply(ctx context.Context, opts *applyOpts, client *instrum.Client, urls instrum.BackendURLs, out io.Writer) error {
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

	return nil
}

func applyApp(ctx context.Context, opts *applyOpts, client *instrum.Client, cluster string, app *instrum.AppSpec, urls instrum.BackendURLs, out io.Writer) error {
	remoteResp, err := client.GetAppInstrumentation(ctx, cluster)
	if err != nil {
		return fmt.Errorf("setup/instrumentation: %w", err)
	}

	remoteApp := &instrum.AppSpec{Namespaces: remoteResp.Namespaces}
	diff := instrum.Compare(app, remoteApp)
	if !diff.IsEmpty() {
		return buildOptimisticLockError(cluster, diff)
	}

	if opts.DryRun {
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
	if opts.DryRun {
		fmt.Fprintf(out, "dry-run: would apply spec.k8s for cluster %q\n", cluster)
		return nil
	}

	if err := client.SetK8SInstrumentation(ctx, cluster, *k8s, urls); err != nil {
		return fmt.Errorf("setup/instrumentation: %w", err)
	}
	fmt.Fprintf(out, "applied spec.k8s for cluster %q\n", cluster)
	return nil
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
