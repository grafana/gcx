package loki

import (
	"regexp"
	"time"

	"github.com/grafana/gcx/internal/shared"
)

var (
	rangeVectorPattern = regexp.MustCompile(`\[([^\[\]]+)\]`)
	// The duration capture allows a leading "-": LogQL accepts a negative
	// offset (grafana/loki#7545, e.g. "offset -5m"), which shifts the
	// evaluated window forward (toward/past "now") rather than backward.
	offsetPattern = regexp.MustCompile(`(?i)\boffset\s+(-?[a-zA-Z0-9.]+)`)
)

// MaxLookback returns how much further back than the query's own start
// (from range-vector durations and positive offsets) and how much further
// forward than end (from negative offsets) a LogQL expression can actually
// reach, by scanning for range-vector durations (e.g. "[24h]" in
// "count_over_time({app=\"x\"}[24h])") and "offset <duration>" modifiers
// anywhere in the expression, and returning each direction's maximum depth.
//
// The index-stats pre-flight check only knows the CLI's own --from/--to (or
// the instant-query now-1m..now default) — it has no idea a range vector or
// offset inside the expression makes Loki actually evaluate a wider window
// than that. Undercounting either direction means the cost estimate can
// miss a genuinely expensive query entirely.
//
// This is a conservative, expression-wide maximum, not tied to any one
// selector — an expression combining several range vectors/offsets with
// different selectors may overestimate the window for some of them, but
// never underestimates. That's the safe direction for a check whose job is
// warning before, or refusing, an expensive query. This deliberately
// doesn't parse LogQL fully, for the same reasoning as
// ExtractStreamSelectors.
//
// Scanning runs against a quote-masked copy of expr (see maskQuoted), so a
// line filter like `|= "offset 24h"` or `|= "value[5m]"` can't be mistaken
// for a real offset/range-vector token.
func MaxLookback(expr string) (time.Duration, time.Duration) {
	masked := maskQuoted(expr)

	// Range-vector width and a positive offset stack (a range vector's own
	// width, then that whole window pushed back further by the offset), so
	// their maxes are summed into the back return value, matching this
	// function's original (pre-negative-offset) behavior exactly for
	// expressions with no negative offset.
	var maxRange, maxPositiveOffset, maxNegativeOffset time.Duration

	for _, m := range rangeVectorPattern.FindAllStringSubmatch(masked, -1) {
		if d, err := shared.ParseDuration(m[1]); err == nil && d > maxRange {
			maxRange = d
		}
	}

	for _, m := range offsetPattern.FindAllStringSubmatch(masked, -1) {
		d, err := shared.ParseDuration(m[1])
		if err != nil {
			continue
		}
		if d < 0 {
			if fwd := -d; fwd > maxNegativeOffset {
				maxNegativeOffset = fwd
			}
		} else if d > maxPositiveOffset {
			maxPositiveOffset = d
		}
	}

	return maxRange + maxPositiveOffset, maxNegativeOffset
}
