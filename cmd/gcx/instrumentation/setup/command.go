// Package setup implements the "gcx instrumentation setup <cluster>"
// onboarding command.
package setup

import (
	"fmt"
	"os"

	"github.com/grafana/gcx/internal/fleet"
	instrum "github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// accessPolicyTokenPlaceholder is substituted into the helm command output.
// Minting a real Cloud Access Policy token is out of scope for setup.
//
//nolint:gosec // Not a real credential — this is a placeholder string shown in user-facing output.
const accessPolicyTokenPlaceholder = "<YOUR_CLOUD_ACCESS_TOKEN>"

// opts holds all flag values parsed by Command.
type opts struct {
	useDefaults   bool
	printHelmOnly bool

	// Per-flag overrides for --use-defaults mode. Canonical idiom is
	// --feat=true|false; no paired --no-* variants exist.
	costMetrics   bool
	clusterEvents bool
	energyMetrics bool
	nodeLogs      bool

	// *Set fields record whether the corresponding flag was explicitly supplied
	// by the caller (via cmd.Flags().Changed). Used by resolveYes to distinguish
	// "not passed" from "passed as false" (pflag cannot distinguish these on bool
	// fields without the Changed check).
	costMetricsSet   bool
	clusterEventsSet bool
	energyMetricsSet bool
	nodeLogsSet      bool
}

func (o *opts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&o.useDefaults, "use-defaults", false,
		"Apply defaults without prompting (required when stdin is not a TTY)")
	flags.BoolVar(&o.printHelmOnly, "print-helm-only", false,
		"Print the helm command and exit; no server calls are made")

	flags.BoolVar(&o.costMetrics, "cost-metrics", false,
		"Set costMetrics under --use-defaults. Pass --cost-metrics=false to disable. Omit to use default (true).")
	flags.BoolVar(&o.clusterEvents, "cluster-events", false,
		"Set clusterEvents under --use-defaults. Pass --cluster-events=false to disable. Omit to use default (true).")
	flags.BoolVar(&o.energyMetrics, "energy-metrics", false,
		"Set energyMetrics under --use-defaults. Pass --energy-metrics=false to disable. Omit to use default (false).")
	flags.BoolVar(&o.nodeLogs, "node-logs", false,
		"Set nodeLogs under --use-defaults. Pass --node-logs=false to disable. Omit to use default (false).")
}

// Validate is a no-op for the setup opts. The mutually-exclusive flag pair
// validation was removed: the --no-* paired forms were
// dropped, so there are no conflicting pairs to check.
func (o *opts) Validate() error {
	return nil
}

// Command returns the "gcx instrumentation setup <cluster>" cobra command.
// loader is used to resolve fleet configuration and backend URLs from the
// active gcx context.
func Command(loader fleet.ConfigLoader) *cobra.Command {
	o := &opts{}
	cmd := &cobra.Command{
		Use:   "setup <cluster>",
		Short: "Onboard a Kubernetes cluster for Grafana Instrumentation Hub",
		Long: `Onboard a Kubernetes cluster end-to-end by configuring K8s monitoring
and printing a runnable helm install command.

Steps performed:
  1. Calls SetupK8sDiscovery — idempotent, safe to re-run.
  2. Reads the cluster's current K8s monitoring configuration.
  3. Resolves desired flag values: prompts interactively (when stdin is a TTY
     and --use-defaults is absent) or applies defaults under --use-defaults:
       costMetrics=true  clusterEvents=true  energyMetrics=false  nodeLogs=false
     Per-flag overrides (--cost-metrics[=true|false], --cluster-events[=true|false],
     --energy-metrics[=true|false], --node-logs[=true|false]) take precedence over defaults.
  4. Calls SetK8SInstrumentation only when at least one flag changed.
  5. Emits a mutation summary to stderr.
  6. Prints a parameterized helm command to stdout.

Re-running with unchanged inputs is safe and idempotent.

The helm command installs grafana-cloud-onboarding and connects the cluster to
Grafana Cloud via Fleet Management. Replace <YOUR_CLOUD_ACCESS_TOKEN> with a
Cloud Access Policy token scoped to metrics:read and set:alloy-data-write.`,

		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Capture which flags were explicitly supplied. pflag cannot
			// distinguish "not passed" from "--flag=false" on plain bool fields;
			// Changed() is the only reliable way to detect explicit false.
			o.costMetricsSet = cmd.Flags().Changed("cost-metrics")
			o.clusterEventsSet = cmd.Flags().Changed("cluster-events")
			o.energyMetricsSet = cmd.Flags().Changed("energy-metrics")
			o.nodeLogsSet = cmd.Flags().Changed("node-logs")

			if err := o.Validate(); err != nil {
				return err
			}
			cluster := args[0]
			ctx := cmd.Context()

			// Load fleet configuration to resolve stack URLs.
			// This is config loading — not an instrumentation API call.
			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("setup: %w", err)
			}
			urls := instrum.BackendURLsFromStack(r.Stack)
			promHdrs := instrum.PromHeadersFromStack(r.Stack)
			fm := instrum.FleetManagementFromStack(r.Stack)

			rn := &runner{
				urls:     urls,
				fm:       fm,
				promHdrs: promHdrs,
				token:    accessPolicyTokenPlaceholder,
				orgSlug:  r.Stack.OrgSlug,
				stdout:   cmd.OutOrStdout(),
				stderr:   cmd.ErrOrStderr(),
				isTTY:    term.IsTerminal(int(os.Stdin.Fd())),
				promptFn: defaultPromptFn(cmd.InOrStdin(), cmd.ErrOrStderr()),
			}

			// Do NOT construct the instrumentation Client when --print-helm-only
			// is set: structural guarantee that no API calls can occur.
			if !o.printHelmOnly {
				rn.client = instrum.NewClient(r.Client)
			}

			return run(ctx, o, cluster, rn)
		},
	}
	o.setup(cmd.Flags())
	return cmd
}
