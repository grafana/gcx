package loki

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/dustin/go-humanize"
	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
)

// statsSelectorConcurrency bounds how many selectors' index-stats calls run
// at once, per the repo's batch-I/O convention (AGENTS.md: bounded errgroup,
// default 10). It's also what stops one slow selector from starving every
// other selector's share of a shared deadline — serially, a slow first
// selector could consume the whole statsPreflightTimeout budget before a
// later, genuinely over-threshold selector ever got to run.
const statsSelectorConcurrency = 10

// statsPreflightOpts holds the --skip-stats/--stats-warn-bytes flags shared
// by the query and metrics commands.
type statsPreflightOpts struct {
	SkipStats      bool
	StatsWarnBytes string

	// warnBytes is StatsWarnBytes parsed by Validate.
	warnBytes uint64
}

func (opts *statsPreflightOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&opts.SkipStats, "skip-stats", false, "Skip the index-stats estimate that is otherwise reported before every query")
	flags.StringVar(&opts.StatsWarnBytes, "stats-warn-bytes", "100GiB", "Warn, and suggest narrowing the query, if index-stats reports more than this many bytes would be scanned (e.g. '500MiB', '2GiB')")
}

// Validate parses --stats-warn-bytes eagerly so an invalid
// value is rejected as an actionable usage error before any network I/O,
// rather than silently disabling the pre-flight check. This is independent of
// --skip-stats: a bad flag value is a user input error regardless of whether
// the check runs.
func (opts *statsPreflightOpts) Validate() error {
	warnBytes, err := humanize.ParseBytes(opts.StatsWarnBytes)
	if err != nil {
		return fmt.Errorf("invalid --stats-warn-bytes: %w", err)
	}
	opts.warnBytes = warnBytes
	return nil
}

// statsPreflightTimeout bounds the whole pre-flight check (all selectors
// combined) so a slow or unresponsive index-stats endpoint can never hold up
// the real query it's advising about. A var, not a const, so tests can lower
// it. index/stats is meant to be a cheap index-only lookup, so 5s is already
// generous for a healthy datasource.
var statsPreflightTimeout = 5 * time.Second //nolint:gochecknoglobals // test-overridable timeout

// resolveStatsWindow returns the [start, end] window to check via
// index/stats for expr, given the query's own time range. For an instant
// query (isRange false) it defaults to the same now-1m..now window
// buildQueryBody already applies. The window is then widened by any
// range-vector duration or offset found in expr (see loki.MaxLookback) —
// positive offsets and range vectors push start further back, while a
// negative offset (LogQL accepts "offset -5m") pushes end further forward —
// since Loki evaluates a wider window than start/end alone would suggest.
// Shared by runStatsPreflight and StatsCmd so the two can't drift.
func resolveStatsWindow(expr string, isRange bool, start, end, now time.Time) (time.Time, time.Time) {
	if !isRange {
		start, end = now.Add(-time.Minute), now
	}
	back, forward := loki.MaxLookback(expr)
	if back > 0 {
		start = start.Add(-back)
	}
	if forward > 0 {
		end = end.Add(forward)
	}
	return start, end
}

// computeStatsBytes sums index-stats bytes across every stream selector
// found in expr, over the window resolveStatsWindow derives from the query's
// own time range.
//
// Selectors are queried concurrently (bounded by statsSelectorConcurrency),
// not serially: all of them share one statsPreflightTimeout deadline, so a
// slow first selector must not be able to consume the whole budget and
// starve a later one — that would silently omit a genuinely over-threshold
// selector's bytes from the total.
//
// The check is advisory: a query with no extractable selector reports
// ok=false, and a failure or timeout calling IndexStats for any individual
// selector is a soft fail for that selector — the remaining selectors'
// results (if any) still count toward the total; ok is true as long as at
// least one selector succeeded.
func computeStatsBytes(ctx context.Context, client *loki.Client, datasourceUID, expr string, isRange bool, start, end, now time.Time) (uint64, bool) {
	ctx, cancel := context.WithTimeout(ctx, statsPreflightTimeout)
	defer cancel()

	start, end = resolveStatsWindow(expr, isRange, start, end, now)

	selectors := loki.ExtractStreamSelectors(expr)
	if len(selectors) == 0 {
		return 0, false
	}

	bytesPerSelector := make([]uint64, len(selectors))
	okPerSelector := make([]bool, len(selectors))

	var g errgroup.Group
	g.SetLimit(statsSelectorConcurrency)
	for i, selector := range selectors {
		g.Go(func() error {
			resp, err := client.IndexStats(ctx, datasourceUID, selector, start, end)
			if err != nil {
				return nil //nolint:nilerr // deliberate soft fail per selector — must not cancel the group
			}
			bytesPerSelector[i] = resp.Bytes
			okPerSelector[i] = true
			return nil
		})
	}
	_ = g.Wait() // every Go func above always returns nil

	var totalBytes uint64
	succeeded := false
	for i, ok := range okPerSelector {
		if ok {
			succeeded = true
			totalBytes += bytesPerSelector[i]
		}
	}
	return totalBytes, succeeded
}

// runStatsPreflight calls computeStatsBytes and reports the estimate to
// stderr: an info line for every query, upgraded to a warning that suggests
// narrowing the query when the sum exceeds warnBytes. Callers must check
// SkipStats themselves before calling — this never skips on its own, so no
// goroutine needs spawning when the check is off. warnBytes is expected to
// already be resolved (e.g. via statsPreflightOpts.Validate).
func runStatsPreflight(ctx context.Context, client *loki.Client, stderr io.Writer, datasourceUID, expr string, isRange bool, start, end, now time.Time, warnBytes uint64) {
	totalBytes, ok := computeStatsBytes(ctx, client, datasourceUID, expr, isRange, start, end, now)
	if !ok {
		return
	}

	if totalBytes > warnBytes {
		cmdio.Warning(stderr, "query may scan approximately %s of data (threshold: %s); consider adding more filters or reducing the time range, or use --skip-stats to skip this check",
			humanize.IBytes(totalBytes), humanize.IBytes(warnBytes))
		return
	}
	cmdio.Info(stderr, "query may scan approximately %s of data; use --skip-stats to skip this check", humanize.IBytes(totalBytes))
}

// reportStatsPreflight runs the estimate before the real query so the
// result is always printed first. It's bounded by statsPreflightTimeout, never
// blocks the query, and does nothing when --skip-stats is set.
func reportStatsPreflight(ctx context.Context, client *loki.Client, stderr io.Writer, datasourceUID, expr string, isRange bool, start, end, now time.Time, preflight *statsPreflightOpts) {
	if preflight.SkipStats {
		return
	}
	runStatsPreflight(ctx, client, stderr, datasourceUID, expr, isRange, start, end, now, preflight.warnBytes)
}
