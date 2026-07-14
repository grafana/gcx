package clusters

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/fleet"
	"github.com/grafana/gcx/internal/gcxerrors"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	"github.com/grafana/gcx/internal/providers/instrumentation/enumerate"
	instrOutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	waitPollInterval   = 5 * time.Second
	waitDefaultTimeout = 5 * time.Minute
)

// errInstrumentationError is returned when INSTRUMENTATION_ERROR is observed
// during wait. Causes a non-zero exit.
var errInstrumentationError = errors.New("cluster reached INSTRUMENTATION_ERROR status")

type waitOpts struct {
	Timeout   time.Duration
	agentMode bool
	// pollInterval controls how often RunK8sMonitoring is polled. Defaults to
	// waitPollInterval (5s). Exposed as a field so tests can override
	// without flag machinery.
	pollInterval time.Duration
}

func (o *waitOpts) setup(flags *pflag.FlagSet) {
	flags.DurationVar(&o.Timeout, "timeout", waitDefaultTimeout, "Maximum time to wait for INSTRUMENTED status")
}

func (o *waitOpts) Validate() error {
	if o.Timeout <= 0 {
		return errors.New("clusters wait: --timeout must be positive")
	}
	return nil
}

func (o *waitOpts) effectivePollInterval() time.Duration {
	if o.pollInterval > 0 {
		return o.pollInterval
	}
	return waitPollInterval
}

func newWaitCommand(loader fleet.ConfigLoader) *cobra.Command {
	opts := &waitOpts{}
	cmd := &cobra.Command{
		Use:   "wait <cluster>",
		Short: "Wait until a cluster reaches INSTRUMENTED status",
		Long: `Poll until the specified cluster reaches INSTRUMENTED status.

The command polls RunK8sMonitoring every 5 seconds. Before starting
the polling loop, it performs a pre-flight check to verify the cluster has
been declared (via gcx instrumentation setup) — if not configured, it
returns an error immediately with a remediation hint.

Exit codes:
  0  Cluster reached INSTRUMENTED status (or a non-error terminal state)
  1  Timeout reached before INSTRUMENTED status
  1  INSTRUMENTATION_ERROR status observed
  1  Pre-flight check failed (cluster not declared)`,
		Args: cobra.ExactArgs(1),
		Example: `  # Wait with default 5-minute timeout
  gcx instrumentation clusters wait prod-eu

  # Wait with a custom timeout
  gcx instrumentation clusters wait prod-eu --timeout 10m`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			opts.agentMode = agent.IsAgentMode()
			ctx := cmd.Context()
			clusterName := args[0]

			base, _, err := fleet.LoadClient(ctx, loader)
			if err != nil {
				return fmt.Errorf("clusters wait: %w", err)
			}
			client := instrumentation.NewClient(base)

			monClient := &monitoringAdapter{client: client}

			return runWait(ctx, opts, client, monClient, clusterName, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	opts.setup(cmd.Flags())
	return cmd
}

// runWait implements the core wait logic. Separated from newWaitCommand for
// testability with fake clients.
//
// stdout receives the final WaitResult envelope (agent mode) or success message.
// stderr receives all progress updates (banner, per-poll status).
func runWait(
	ctx context.Context,
	opts *waitOpts,
	declaredClient declaredStateClient,
	monClient enumerate.MonitoringClient,
	clusterName string,
	stdout io.Writer,
	stderr io.Writer,
) error {
	// Pre-flight: verify cluster is declared.
	// Absent declaration = permanent user error; fail-fast with remediation hint.
	// Pipeline not yet materialized = transient; must poll.
	resp, err := declaredClient.GetK8SInstrumentation(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("clusters wait: pre-flight: %w", err)
	}
	if resp.Cluster.Name == "" {
		return &gcxerrors.DetailedError{
			Summary: "cluster is not declared",
			Details: fmt.Sprintf("cluster %q has no K8s monitoring configuration", clusterName),
			Suggestions: []string{
				fmt.Sprintf("Run: gcx instrumentation setup %s --use-defaults", clusterName),
			},
		}
	}

	// Emit banner to stderr: all human progress routes to stderr.
	banner := instrOutput.WaitBanner{
		Event:   "waiting_started",
		Target:  instrOutput.Target{Cluster: clusterName},
		Timeout: opts.Timeout.String(),
	}
	_ = banner.EmitTo(stderr, opts.agentMode)

	start := time.Now()
	timeoutCh := time.After(opts.Timeout)
	ticker := time.NewTicker(opts.effectivePollInterval())
	defer ticker.Stop()

	var lastStatus instrumentation.InstrumentationStatus

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("clusters wait: %w", ctx.Err())
		case <-timeoutCh:
			// Emit fused WaitResult with Error field to stdout, then
			// return ErrWaitTimeoutEmitted so the fail converter suppresses the
			// secondary DetailedError envelope.
			timeoutMsg := fmt.Sprintf(
				"timeout after %s waiting for cluster %q to reach INSTRUMENTED — "+
					"alloy-daemon may not have registered with Fleet Management yet; "+
					"check 'helm status grafana-cloud -n monitoring' and 'kubectl logs -n monitoring -l app.kubernetes.io/name=alloy-daemon --context <ctx>'",
				opts.Timeout, clusterName)
			result := instrOutput.WaitResultForCluster("timeout", clusterName, string(lastStatus), start)
			result.Error = &instrOutput.WaitError{
				Summary:  fmt.Sprintf("timeout waiting for cluster %q", clusterName),
				Details:  timeoutMsg,
				ExitCode: 1,
			}
			_ = result.Emit(stdout, opts.agentMode)
			return fmt.Errorf("clusters wait: %w", instrumentation.ErrWaitTimeoutEmitted)
		case <-ticker.C:
			clusters, err := monClient.RunK8sMonitoring(ctx)
			if err != nil {
				fmt.Fprintf(stderr, "  poll error (retrying): %v\n", err)
				continue
			}

			current := findClusterStatus(clusters, clusterName)
			lastStatus = current

			// Emit per-poll progress to stderr.
			progress := instrOutput.WaitProgress{
				Event:     "waiting",
				Target:    instrOutput.Target{Cluster: clusterName},
				Status:    string(current),
				ElapsedMs: time.Since(start).Milliseconds(),
			}
			_ = progress.EmitTo(stderr, opts.agentMode)

			// Use typed classifier to match full proto enum names from wire
			// (e.g., "K8S_MONITORING_STATUS_INSTRUMENTED", not "INSTRUMENTED").
			switch instrumentation.ClassifyK8sMonitoringStatus(current) {
			case instrumentation.WaitSuccess:
				result := instrOutput.WaitResultForCluster("success", clusterName, string(current), start)
				return result.Emit(stdout, opts.agentMode)

			case instrumentation.WaitError:
				// INSTRUMENTATION_ERROR must exit non-zero immediately.
				return fmt.Errorf("clusters wait: %w", errInstrumentationError)

			default:
				// WaitPending: continue polling (PENDING_INSTRUMENTATION, PENDING_UNINSTRUMENTATION, etc.).
			}
		}
	}
}

// findClusterStatus returns the InstrumentationStatus for clusterName from the
// monitoring response. Returns StatusNotInstrumented if not found.
func findClusterStatus(clusters []instrumentation.ClusterObservedState, clusterName string) instrumentation.InstrumentationStatus {
	for _, c := range clusters {
		if c.Name == clusterName {
			return c.InstrumentationStatus
		}
	}
	return instrumentation.StatusNotInstrumented
}
