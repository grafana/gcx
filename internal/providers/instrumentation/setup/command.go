package setup

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/fleet"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/providers"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type setupOpts struct {
	Defaults bool
}

func (o *setupOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVarP(&o.Defaults, "defaults", "y", false, "Accept all defaults without prompting (non-interactive / CI mode)")
}

// Validate checks setupOpts for configuration errors.
func (o *setupOpts) Validate() error {
	return nil
}

// safeShellArg returns s as a shell-safe argument. Simple names (alphanumeric,
// hyphens, dots, underscores) are returned as-is; anything else is wrapped in
// single quotes with interior single quotes escaped.
var safeShellName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func safeShellArg(s string) string {
	if safeShellName.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// clusterNamePattern matches the Helm chart's cluster.name requirement:
// lowercase alphanumeric or hyphen, starting and ending with an alphanumeric
// character.
var clusterNamePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validateClusterName(s string) error {
	if s == "" {
		return errors.New("cluster name is required")
	}
	if !clusterNamePattern.MatchString(s) {
		return errors.New("must be lowercase alphanumeric or '-', starting and ending with an alphanumeric character")
	}
	return nil
}

func newCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &setupOpts{}
	cmd := &cobra.Command{
		Use:   "setup [cluster]",
		Short: "Interactive bootstrap for a cluster's instrumentation configuration.",
		Long: `Bootstrap a cluster's instrumentation configuration through a guided flow.

Prompts for the cluster name (when not provided as a positional argument) and
Kubernetes monitoring flags (cost metrics, cluster events, node logs, energy
metrics), then prints the Helm chart installation instructions.

Use --defaults / -y to accept all defaults without prompting (CI-friendly);
in that mode the cluster name must be passed as a positional argument.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			ctx := cmd.Context()

			var clusterName string
			if len(args) == 1 {
				clusterName = args[0]
			}

			nonInteractive := opts.Defaults || !terminal.StdoutIsTerminal()
			if nonInteractive && clusterName == "" {
				return errors.New("instrumentation: cluster name required in non-interactive mode; pass it as a positional argument")
			}

			k8s := instrumentation.K8sSpec{
				CostMetrics:   true,
				ClusterEvents: true,
				NodeLogs:      false,
				EnergyMetrics: false,
			}

			if !nonInteractive {
				if err := runWizard(cmd, &clusterName, &k8s); err != nil {
					if errors.Is(err, huh.ErrUserAborted) {
						cmdio.Info(cmd.ErrOrStderr(), "Aborted.")
						return nil
					}
					return fmt.Errorf("instrumentation: %w", err)
				}
			}

			if err := validateClusterName(clusterName); err != nil {
				return fmt.Errorf("instrumentation: cluster name: %w", err)
			}

			r, err := fleet.LoadClientWithStack(ctx, loader)
			if err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			client := instrumentation.NewClient(r.Client)
			urls := instrumentation.BackendURLsFromStack(r.Stack)

			stderr := cmd.ErrOrStderr()
			fmt.Fprintf(stderr, "\nK8s monitoring configuration for cluster %q:\n", clusterName)
			fmt.Fprintf(stderr, "  cost metrics:   %v\n", k8s.CostMetrics)
			fmt.Fprintf(stderr, "  cluster events: %v\n", k8s.ClusterEvents)
			fmt.Fprintf(stderr, "  node logs:      %v\n", k8s.NodeLogs)
			fmt.Fprintf(stderr, "  energy metrics: %v\n", k8s.EnergyMetrics)

			if err := client.SetK8SInstrumentation(ctx, clusterName, k8s, urls); err != nil {
				return fmt.Errorf("instrumentation: %w", err)
			}

			cmdio.Success(stderr, "K8s monitoring configured for cluster %q.", clusterName)
			// F8: printPostApplyHint is guidance/hints, not a result — write to stderr.
			printPostApplyHint(stderr, clusterName, r.Stack)
			return nil
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// Command returns the setup cobra command.
func Command(loader *providers.ConfigLoader) *cobra.Command {
	return newCommand(loader)
}

// runWizard drives the interactive setup form via huh, populating clusterName
// (when empty) and the four K8sSpec flags. The form reads from the command's
// stdin and writes to its stderr. Accessible (line-oriented) mode is enabled
// when stdout is not a TTY so the wizard remains driveable from piped shells.
func runWizard(cmd *cobra.Command, clusterName *string, k8s *instrumentation.K8sSpec) error {
	var fields []huh.Field

	if *clusterName == "" {
		fields = append(fields, huh.NewInput().
			Title("What's your cluster name?").
			Description("Lowercase alphanumeric or '-', starting and ending with an alphanumeric character.").
			Validate(validateClusterName).
			Value(clusterName),
		)
	}

	fields = append(fields,
		huh.NewConfirm().Title("Enable cost metrics?").Affirmative("Yes").Negative("No").Value(&k8s.CostMetrics),
		huh.NewConfirm().Title("Enable cluster events?").Affirmative("Yes").Negative("No").Value(&k8s.ClusterEvents),
		huh.NewConfirm().Title("Enable node logs?").Affirmative("Yes").Negative("No").Value(&k8s.NodeLogs),
		huh.NewConfirm().Title("Enable energy metrics?").Affirmative("Yes").Negative("No").Value(&k8s.EnergyMetrics),
	)

	form := huh.NewForm(huh.NewGroup(fields...)).
		WithInput(cmd.InOrStdin()).
		WithOutput(cmd.ErrOrStderr()).
		WithAccessible(!terminal.StdoutIsTerminal())
	return form.Run()
}

// printPostApplyHint prints guidance on how to connect the cluster to Grafana Cloud
// via the grafana-cloud-onboarding Helm chart.
// F8: writes to stderr (hints/guidance, not a result).
// F21: cluster name is shell-quoted via safeShellArg.
func printPostApplyHint(w io.Writer, cluster string, stack cloud.StackInfo) {
	fleetURL := stack.AgentManagementInstanceURL
	if fleetURL == "" {
		return
	}
	instanceID := stack.AgentManagementInstanceID
	quotedCluster := safeShellArg(cluster)
	fmt.Fprintf(w, "\nTo connect cluster %q to Grafana Cloud, install the Helm chart:\n\n", cluster)
	fmt.Fprintf(w, "  1. Create an access policy token with scopes: metrics:read, set:alloy-data-write\n")
	fmt.Fprintf(w, "     https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/create-access-policies/\n\n")
	fmt.Fprintf(w, "  2. Install the Helm chart:\n\n")
	fmt.Fprintf(w, "       helm repo add grafana https://grafana.github.io/helm-charts\n")
	fmt.Fprintf(w, "       helm upgrade --install grafana-cloud -n monitoring --create-namespace \\\n")
	fmt.Fprintf(w, "         grafana/grafana-cloud-onboarding \\\n")
	fmt.Fprintf(w, "         --set \"cluster.name=%s\" \\\n", quotedCluster)
	fmt.Fprintf(w, "         --set \"grafanaCloud.fleetManagement.url=%s\" \\\n", fleetURL)
	fmt.Fprintf(w, "         --set \"grafanaCloud.fleetManagement.auth.username=%d\" \\\n", instanceID)
	fmt.Fprintf(w, "         --set \"grafanaCloud.fleetManagement.auth.password=<YOUR_CLOUD_ACCESS_TOKEN>\" \\\n")
	fmt.Fprintf(w, "         --wait\n")

	var footer instrumentation.Footer
	footer.Notef("Cluster %q is persisted; list/get shows Alloy collector-reported state and may take ~30s to refresh.", cluster)
	footer.Hint("to verify", "gcx instrumentation status --cluster "+safeShellArg(cluster))
	footer.Print(w)
}
