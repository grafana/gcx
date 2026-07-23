package apps

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/grafana/gcx/internal/agent"
	"github.com/grafana/gcx/internal/providers/instrumentation"
	instrOutput "github.com/grafana/gcx/internal/providers/instrumentation/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// pollInterval is the wait poll cadence matching the collector-app UI.
const pollInterval = 5 * time.Second

type waitOpts struct {
	timeout time.Duration
	// pollInterval controls how often RunK8sDiscovery is polled. Defaults to
	// pollInterval (5s). Exposed as a field so tests can override without
	// flag machinery.
	pollInterval time.Duration
}

func (o *waitOpts) setup(flags *pflag.FlagSet) {
	flags.DurationVar(&o.timeout, "timeout", 5*time.Minute, "Maximum time to wait (e.g. 5m, 10m, 1h)")
}

func (o *waitOpts) Validate() error {
	if o.timeout <= 0 {
		return errors.New("apps wait: --timeout must be positive")
	}
	return nil
}

func (o *waitOpts) effectivePollInterval() time.Duration {
	if o.pollInterval > 0 {
		return o.pollInterval
	}
	return pollInterval
}

// makeWaitCmd builds the "apps wait <cluster> <namespace>" command.
//
//   - Polls RunK8sDiscovery at 5-second intervals, filtering client-side to (cluster, namespace).
//   - Aggregates per-workload statuses to namespace level.
//   - Exits 0 when the namespace transitions to a non-pending, non-error state.
//   - Exits non-zero on INSTRUMENTATION_ERROR or timeout.
//   - Default timeout: 5m.
//
// Aggregation rule:
//   - No items for (cluster, namespace) → treat as PENDING_INSTRUMENTATION (keep polling).
//   - Any item with INSTRUMENTATION_ERROR → exit non-zero immediately.
//   - Any item with PENDING_INSTRUMENTATION or PENDING_UNINSTRUMENTATION → keep polling.
//   - Otherwise (all INSTRUMENTED, EXCLUDED, or other terminal non-error states) → exit 0.
//
// factory is called inside RunE — after cobra has parsed all flags — to
// lazily construct the appsClient and PromHeaders.
func makeWaitCmd(factory appClientFactory) *cobra.Command {
	return makeWaitCmdWithOpts(factory, &waitOpts{})
}

// makeWaitCmdWithOpts is the injectable core of makeWaitCmd; tests pass opts
// with a fast pollInterval override.
func makeWaitCmdWithOpts(factory appClientFactory, opts *waitOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wait <cluster> <namespace>",
		Short: "Wait for namespace instrumentation to reach a stable state",
		Long: `Wait for the namespace Beyla instrumentation to transition out of a pending
state by polling RunK8sDiscovery at 5-second intervals.

Exit 0 when the namespace's workloads reach a stable non-pending state
(INSTRUMENTED, NOT_INSTRUMENTED, EXCLUDED, or any terminal non-error state).

Exit non-zero when:
  - INSTRUMENTATION_ERROR is observed for any workload.
  - The --timeout duration elapses while still in a pending state.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Validate(); err != nil {
				return err
			}
			cluster := args[0]
			namespace := args[1]

			timeout := opts.timeout

			ctx := cmd.Context()
			client, _, promHeaders, err := factory(ctx)
			if err != nil {
				return err
			}

			// Capture agent mode once at call site.
			agentMode := agent.IsAgentMode()
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			start := time.Now()
			deadline := time.Now().Add(timeout)
			ticker := time.NewTicker(opts.effectivePollInterval())
			defer ticker.Stop()

			var lastRawStatus instrumentation.InstrumentationStatus

			// lastPollErr remembers a persistent poll failure (bad token, DNS)
			// so the timeout terminal names the real cause instead of
			// reporting a bare timeout with the cause buried in stderr retry
			// lines. Cleared on any successful poll.
			var lastPollErr error

			target := instrOutput.Target{Cluster: cluster, Namespace: namespace}

			// terminal emits the fused terminal WaitResult failure envelopes
			// (timeout, error status, cancellation) shared with `clusters wait`.
			terminal := instrOutput.WaitTerminal{
				Target:    target,
				ErrPrefix: "apps wait",
				Start:     start,
				Stdout:    stdout,
				AgentMode: agentMode,
			}

			// finishTimeout emits the fused terminal timeout WaitResult; the
			// ErrWaitTimeoutEmitted sentinel is returned only when that write
			// lands intact (see WaitTerminal.FinishTimeout).
			finishTimeout := func() error {
				timeoutMsg := fmt.Sprintf("timed out after %s waiting for namespace %q in cluster %q%s",
					timeout, namespace, cluster, probePipelineMsg(ctx, client, cluster))
				if lastPollErr != nil {
					timeoutMsg = fmt.Sprintf("%s; last poll error: %v", timeoutMsg, lastPollErr)
				}
				return terminal.FinishTimeout(string(lastRawStatus),
					fmt.Sprintf("timed out waiting for namespace %q in cluster %q", namespace, cluster),
					timeoutMsg)
			}

			// finishCanceled (agent mode only) writes the terminal canceled
			// stream_end line and returns the matching EmittedError (see
			// WaitTerminal.FinishCanceled).
			finishCanceled := func(cause error) error {
				return terminal.FinishCanceled(cause, string(lastRawStatus),
					"wait canceled before reaching a stable status")
			}

			for {
				// outcome is WaitOutcome; rawStatus is the wire string for logging.
				outcome, rawStatus, pollErr := pollNamespaceStatus(ctx, client, promHeaders, cluster, namespace)
				switch {
				case pollErr != nil && ctx.Err() != nil:
					// A cancellation arriving mid-poll surfaces as a poll error
					// wrapping context.Canceled — route it to the canceled
					// terminal so agent mode still gets its gcx.stream_end
					// outcome=canceled line; human mode keeps the plain
					// cancellation error.
					if agentMode {
						return finishCanceled(ctx.Err())
					}
					return ctx.Err()
				case pollErr != nil:
					// Transient poll failure: emit the typed poll_error stream
					// event (agent mode) or the human retry line, then keep
					// polling until the deadline.
					lastPollErr = pollErr
					pe := instrOutput.WaitPollError{
						Event:  "poll_error",
						Target: target,
						Error:  pollErr.Error(),
					}
					_ = pe.EmitTo(stderr, agentMode)
				default:
					lastPollErr = nil
					lastRawStatus = rawStatus

					switch outcome {
					case instrumentation.WaitError:
						// INSTRUMENTATION_ERROR must exit non-zero immediately.
						// Agent mode gets the fused terminal stream_end line first;
						// human mode keeps the plain error the reporter renders.
						errStatus := fmt.Errorf("apps wait: namespace %q in cluster %q reached INSTRUMENTATION_ERROR", namespace, cluster)
						if !agentMode {
							return errStatus
						}
						return terminal.FinishErrorStatus(errStatus, string(rawStatus),
							fmt.Sprintf("namespace %q in cluster %q reached INSTRUMENTATION_ERROR", namespace, cluster),
							fmt.Sprintf("namespace %q in cluster %q reported status %s", namespace, cluster, rawStatus))
					case instrumentation.WaitPending:
						// Emit per-poll progress to stderr.
						progress := instrOutput.WaitProgress{
							Event:     "waiting",
							Target:    target,
							Status:    string(rawStatus),
							ElapsedMs: time.Since(start).Milliseconds(),
						}
						_ = progress.EmitTo(stderr, agentMode)
					default: // WaitSuccess
						result := instrOutput.WaitResult{
							Outcome:   "success",
							Target:    target,
							Status:    string(rawStatus),
							ElapsedMs: time.Since(start).Milliseconds(),
						}
						return result.Emit(stdout, agentMode)
					}
				}

				remaining := time.Until(deadline)
				if remaining <= 0 {
					return finishTimeout()
				}

				select {
				case <-ctx.Done():
					// Agent mode still owes the stream its terminal event;
					// human mode keeps the plain cancellation error.
					if agentMode {
						return finishCanceled(ctx.Err())
					}
					return ctx.Err()
				case <-time.After(remaining):
					return finishTimeout()
				case <-ticker.C:
				}
			}
		},
	}

	opts.setup(cmd.Flags())
	return cmd
}

// newWaitCmd is a test-facing constructor that injects a pre-built appsClient
// and polls at a fast test cadence. Production code uses
// makeWaitCmd(factoryFromLoader(loader)) instead.
func newWaitCmd(client appsClient) *cobra.Command {
	return makeWaitCmdWithOpts(func(_ context.Context) (appsClient, instrumentation.BackendURLs, instrumentation.PromHeaders, error) {
		return client, instrumentation.BackendURLs{}, instrumentation.PromHeaders{}, nil
	}, &waitOpts{pollInterval: time.Millisecond})
}

// probePipelineMsg checks whether a Fleet Management pipeline named
// "beyla_k8s_appo11y_<cluster>" exists and returns a diagnostic suffix for
// timeout error messages. Returns an empty string when ListPipelines fails so
// that the diagnostic enrichment never obscures the original timeout cause.
func probePipelineMsg(ctx context.Context, client appsClient, cluster string) string {
	pipelineName := "beyla_k8s_appo11y_" + cluster
	pipelines, err := client.ListPipelines(ctx)
	if err != nil {
		return ""
	}
	for _, p := range pipelines {
		if p.Name == pipelineName {
			return fmt.Sprintf("; Fleet Management pipeline %q exists (may not yet be synced to alloy-daemon)", pipelineName)
		}
	}
	return fmt.Sprintf("; Fleet Management pipeline %q not found — run 'gcx fleet pipelines list' to verify", pipelineName)
}

// pollNamespaceStatus calls RunK8sDiscovery and aggregates per-workload statuses
// for the (cluster, namespace) pair. Returns the aggregated WaitOutcome, the
// representative raw status string for logging, and any RPC error.
//
// Aggregation rule:
//   - No items → (WaitPending, "INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION", nil)
//   - Any WaitError item → (WaitError, that item's raw status, nil)
//   - Any WaitPending item → (WaitPending, first pending item's raw status, nil)
//   - All WaitSuccess → (WaitSuccess, first success item's raw status, nil)
func pollNamespaceStatus(
	ctx context.Context,
	client appsClient,
	promHeaders instrumentation.PromHeaders,
	cluster, namespace string,
) (instrumentation.WaitOutcome, instrumentation.InstrumentationStatus, error) {
	resp, err := client.RunK8sDiscovery(ctx, promHeaders)
	if err != nil {
		return instrumentation.WaitPending, "", err
	}

	// Filter to (cluster, namespace).
	var items []instrumentation.DiscoveryItem
	for _, item := range resp.Items {
		if item.ClusterName == cluster && item.Namespace == namespace {
			items = append(items, item)
		}
	}

	if len(items) == 0 {
		return instrumentation.WaitPending,
			"INSTRUMENTATION_STATUS_PENDING_INSTRUMENTATION", nil
	}

	hasPending := false
	firstPendingStatus := instrumentation.InstrumentationStatus("")
	firstSuccessStatus := instrumentation.InstrumentationStatus("")

	for _, item := range items {
		switch instrumentation.ClassifyInstrumentationStatus(item.InstrumentationStatus) {
		case instrumentation.WaitError:
			return instrumentation.WaitError, item.InstrumentationStatus, nil
		case instrumentation.WaitPending:
			hasPending = true
			if firstPendingStatus == "" {
				firstPendingStatus = item.InstrumentationStatus
			}
		default: // WaitSuccess
			if firstSuccessStatus == "" {
				firstSuccessStatus = item.InstrumentationStatus
			}
		}
	}

	if hasPending {
		return instrumentation.WaitPending, firstPendingStatus, nil
	}
	return instrumentation.WaitSuccess, firstSuccessStatus, nil
}
