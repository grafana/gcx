package loki

import (
	"regexp"
	"time"

	"github.com/grafana/gcx/internal/shared"
)

var (
	rangeVectorPattern = regexp.MustCompile(`\[([^\[\]]+)\]`)
	offsetPattern      = regexp.MustCompile(`(?i)\boffset\s+([a-zA-Z0-9.]+)`)
)

// MaxLookback returns how much further back than the query's own start/end a
// LogQL expression can actually reach, by scanning for range-vector
// durations (e.g. "[24h]" in "count_over_time({app=\"x\"}[24h])") and
// "offset <duration>" modifiers anywhere in the expression, and returning
// their maximum combined depth.
//
// The index-stats pre-flight check only knows the CLI's own --from/--to (or
// the instant-query now-1m..now default) — it has no idea a range vector or
// offset inside the expression makes Loki actually evaluate further back
// than that. Undercounting that window means the cost estimate can miss a
// genuinely expensive query entirely.
//
// This is a conservative, expression-wide maximum, not tied to any one
// selector — an expression combining several range vectors/offsets with
// different selectors may overestimate the window for some of them, but
// never underestimates. That's the safe direction for a check whose only
// job is warning before an expensive query. This deliberately doesn't parse
// LogQL fully, for the same reasoning as ExtractStreamSelectors.
func MaxLookback(expr string) time.Duration {
	var maxRange, maxOffset time.Duration

	for _, m := range rangeVectorPattern.FindAllStringSubmatch(expr, -1) {
		if d, err := shared.ParseDuration(m[1]); err == nil && d > maxRange {
			maxRange = d
		}
	}

	for _, m := range offsetPattern.FindAllStringSubmatch(expr, -1) {
		if d, err := shared.ParseDuration(m[1]); err == nil && d > maxOffset {
			maxOffset = d
		}
	}

	return maxRange + maxOffset
}
