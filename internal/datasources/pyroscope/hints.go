package pyroscope

import (
	"fmt"
	"io"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/pyroscope"
)

// emitEmptyWindowHint reports the time window that produced an empty metadata
// result. Without it, an empty tenant and a tenant whose data sits just outside
// the implicit default window are indistinguishable from the output alone.
func emitEmptyWindowHint(w io.Writer, what string, start, end time.Time, explicitRange bool) {
	effStart, effEnd := pyroscope.DefaultTimeRange(start, end)
	window := fmt.Sprintf("%s to %s", effStart.UTC().Format(time.RFC3339), effEnd.UTC().Format(time.RFC3339))
	if explicitRange {
		cmdio.EmitHint(w, fmt.Sprintf("no %s found in window %s — widen the range with --since or --from/--to", what, window), "")
		return
	}
	cmdio.EmitHint(w, fmt.Sprintf("no %s found in the default window (last 1h: %s) — pass --since (e.g. --since 24h) or --from/--to to search further back", what, window), "")
}
