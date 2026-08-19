package experiments

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/grafana/gcx/internal/gcxerrors"
)

// waitPollInterval is how often --wait polls the experiment, the same cadence
// gcx instrumentation clusters wait uses.
const waitPollInterval = 5 * time.Second

// experimentGetter is the one client call the wait loop needs, split out so
// tests can script a sequence of statuses.
type experimentGetter interface {
	Get(ctx context.Context, runID string) (*Experiment, error)
}

// waitForFinished polls the experiment until it reaches a terminal status
// (completed, failed or canceled), the timeout elapses, or ctx is canceled.
//
// The first poll happens immediately and any error there returns right away, so
// a wrong run ID or a broken token fails fast instead of spinning until the
// timeout. A later poll error is treated as transient: the loop keeps polling
// and the timeout error names the last one.
//
// Progress goes to stderr, one line per poll, so a CI log shows the wait is
// alive. The timeout error is a plain DetailedError, so it exits 1 like every
// other not-finished outcome, and no check document is written.
func waitForFinished(ctx context.Context, client experimentGetter, runID string, timeout, interval time.Duration, stderr io.Writer) error {
	start := time.Now()

	run, err := client.Get(ctx, runID)
	if err != nil {
		return err
	}
	lastStatus := run.Status
	if isFinishedStatus(lastStatus) {
		return nil
	}
	emitWaitProgress(stderr, runID, lastStatus, start)

	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastPollErr error
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeoutCh:
			details := fmt.Sprintf("experiment %s still reports status %q after %s", runID, lastStatus, timeout)
			if lastPollErr != nil {
				details = fmt.Sprintf("%s; last poll error: %v", details, lastPollErr)
			}
			return &gcxerrors.DetailedError{
				Summary: "Timed out waiting for the experiment to finish",
				Details: details,
				Suggestions: []string{
					"Raise --timeout for a long suite",
					fmt.Sprintf("Watch the run with 'gcx agento11y experiments get %s'", runID),
				},
			}
		case <-ticker.C:
			run, err := client.Get(ctx, runID)
			if err != nil {
				lastPollErr = err
				continue
			}
			lastPollErr = nil
			lastStatus = run.Status
			if isFinishedStatus(lastStatus) {
				return nil
			}
			emitWaitProgress(stderr, runID, lastStatus, start)
		}
	}
}

func emitWaitProgress(stderr io.Writer, runID, status string, start time.Time) {
	fmt.Fprintf(stderr, "waiting: experiment %s status %q (elapsed %s)\n",
		runID, status, time.Since(start).Truncate(time.Second))
}
