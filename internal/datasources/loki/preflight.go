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

// statsPreflightOpts holds the --skip-stats/--stats flags shared by the query
// and metrics commands.
type statsPreflightOpts struct {
	SkipStats      bool
	StatsWarnBytes string

	// warnBytes is StatsWarnBytes parsed by Validate.
	warnBytes uint64
}

func (opts *statsPreflightOpts) setup(flags *pflag.FlagSet) {
	flags.BoolVar(&opts.SkipStats, "skip-stats", false, "Skip the index-stats pre-flight check")
	flags.StringVar(&opts.StatsWarnBytes, "stats", "1GiB", "Warn (non-blocking) if index-stats reports more than this many bytes would be scanned (e.g. '500MiB', '2GiB')")
}

// Validate parses --stats eagerly so an invalid value is rejected as an
// actionable usage error before any network I/O, rather than silently
// disabling the pre-flight warning. This is independent of --skip-stats: a bad
// flag value is a user input error regardless of whether the check runs.
func (opts *statsPreflightOpts) Validate() error {
	warnBytes, err := humanize.ParseBytes(opts.StatsWarnBytes)
	if err != nil {
		return fmt.Errorf("invalid --stats: %w", err)
	}
	opts.warnBytes = warnBytes
	return nil
}

// runStatsPreflight calls Loki's index-stats endpoint once per stream
// selector found in expr and prints a non-blocking warning to stderr if the
// summed byte count exceeds warnBytes. isRange/start/end/now determine the
// window: for an instant query (isRange false) it defaults to the same
// now-1m..now window buildQueryBody already applies, so the estimate and the
// real query look at consistent ranges.
//
// This check is advisory only: skipStats bypasses it entirely, a query with
// no extractable selector is skipped silently, and a failure calling
// IndexStats for any individual selector (older Loki, permissions, endpoint
// unsupported on some self-hosted setups) is a soft fail for that selector —
// the remaining selectors' results (if any) still count toward the total.
// warnBytes is expected to already be resolved (e.g. via
// statsPreflightOpts.Validate) — invalid user input is rejected earlier, not
// handled here.
func runStatsPreflight(ctx context.Context, client *loki.Client, stderr io.Writer, datasourceUID, expr string, isRange bool, start, end, now time.Time, skipStats bool, warnBytes uint64) {
	if skipStats {
		return
	}

	if !isRange {
		start, end = now.Add(-time.Minute), now
	}

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
