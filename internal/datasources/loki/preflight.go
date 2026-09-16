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
)

// statsPreflightOpts holds the --skip-stats/--stats-warn-bytes flags shared
// by the query and metrics commands.
type statsPreflightOpts struct {
	SkipStats      bool
	StatsWarnBytes string

	// warnBytes is StatsWarnBytes parsed by Validate.
	warnBytes uint64
}

func (opts *statsPreflightOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&opts.SkipStats, "skip-stats", false, "Skip the index-stats pre-flight check")
	flags.StringVar(&opts.StatsWarnBytes, "stats-warn-bytes", "1GiB", "Warn (non-blocking) if index-stats reports more than this many bytes would be scanned (e.g. '500MiB', '2GiB')")
}

// Validate parses --stats-warn-bytes eagerly so an invalid value is rejected
// as an actionable usage error before any network I/O, rather than silently
// disabling the pre-flight warning. This is independent of --skip-stats: a bad
// flag value is a user input error regardless of whether the check runs.
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
// range-vector duration or offset found in expr (see loki.MaxLookback),
// since Loki evaluates further back than start/end alone would suggest.
// Shared by runStatsPreflight and StatsCmd so the two can't drift.
func resolveStatsWindow(expr string, isRange bool, start, end, now time.Time) (time.Time, time.Time) {
	if !isRange {
		start, end = now.Add(-time.Minute), now
	}
	if lookback := loki.MaxLookback(expr); lookback > 0 {
		start = start.Add(-lookback)
	}
	return start, end
}

// runStatsPreflight calls Loki's index-stats endpoint once per stream
// selector found in expr and prints a non-blocking warning to stderr if the
// summed byte count exceeds warnBytes. Callers must check SkipStats
// themselves before calling — this never skips on its own, so no goroutine
// needs spawning when the check is off.
//
// This check is advisory only: a query with no extractable selector is
// skipped silently, and a failure or timeout calling IndexStats for any
// individual selector is a soft fail for that selector — the remaining
// selectors' results (if any) still count toward the total. warnBytes is
// expected to already be resolved (e.g. via statsPreflightOpts.Validate).
func runStatsPreflight(ctx context.Context, client *loki.Client, stderr io.Writer, datasourceUID, expr string, isRange bool, start, end, now time.Time, warnBytes uint64) {
	ctx, cancel := context.WithTimeout(ctx, statsPreflightTimeout)
	defer cancel()

	start, end = resolveStatsWindow(expr, isRange, start, end, now)

	selectors := loki.ExtractStreamSelectors(expr)
	if len(selectors) == 0 {
		return
	}

	var totalBytes uint64
	succeeded := false
	for _, selector := range selectors {
		resp, err := client.IndexStats(ctx, datasourceUID, selector, start, end)
		if err != nil {
			continue
		}
		succeeded = true
		totalBytes += resp.Bytes
	}
	if !succeeded {
		return
	}

	if totalBytes > warnBytes {
		cmdio.Warning(stderr, "query may scan approximately %s of data (threshold: %s); use --skip-stats to suppress this check",
			humanize.IBytes(totalBytes), humanize.IBytes(warnBytes))
	}
}
