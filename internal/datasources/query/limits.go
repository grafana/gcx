package query

import (
	"fmt"
	"io"
	"strconv"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	querysql "github.com/grafana/gcx/internal/query/sql"
)

const (
	// DefaultLokiLimit is the default result cap for Loki queries when --limit
	// is not explicitly provided. A smaller value avoids overwhelming output;
	// use --limit 0 for no cap or --limit N for a custom value.
	DefaultLokiLimit = 50
)

// SurfaceRowLimits reports to the user (on stderr, w) when a SQL query's results
// were capped. It handles two independent truncation sources:
//
//  1. gcx's own injected cap. When capped is true (the caller injected a
//     sentinel LIMIT/TOP of eff+1 via EnforceLimitSentinel/EnforceTopSentinel),
//     it drops the surplus rows to eff and, if any were dropped, emits a hint.
//  2. Server-side plugin notices (e.g. the datasource's own "results have been
//     limited to N" warning), surfaced verbatim.
//
// It must be called before encoding resp so the sentinel row never reaches
// output. maxLimit is only used to phrase the hint.
func SurfaceRowLimits(w io.Writer, resp *querysql.QueryResponse, capped bool, eff, maxLimit int) {
	if capped && resp.Truncate(eff) {
		cmdio.EmitHint(w, querysql.TruncationHint(eff, maxLimit), "")
	}
	for _, notice := range resp.Notices {
		cmdio.EmitHint(w, notice, "")
	}
}

// SurfaceLogLimit warns when Loki fills the requested limit; the response does
// not tell us whether more matching lines remain.
func SurfaceLogLimit(w io.Writer, resp *loki.QueryResponse, limit int) {
	if limit <= 0 {
		return
	}
	count := 0
	for _, stream := range resp.Data.Result {
		count += len(stream.Values)
	}
	if count >= limit {
		cmdio.EmitHint(w, fmt.Sprintf("query reached --limit %d; results may be incomplete. Increase --limit to retrieve more log lines", limit), "")
	}
}

// SurfaceDroppedLogs hints that Adaptive Logs sampling dropped matching lines
// and that the time machine may still hold them. command is the follow-up
// invocation that fetches those lines.
func SurfaceDroppedLogs(w io.Writer, resp *loki.QueryResponse, command string) {
	s := loki.SampledLines(resp)
	if !s.Any() {
		return
	}

	var summary string
	switch {
	case s.Lines > 0 && s.MaxDropPercent > 0:
		summary = fmt.Sprintf("%d returned line(s) were sampled by Adaptive Logs (up to %s%% of similar lines dropped); the dropped lines may be recoverable from the time machine",
			s.Lines, strconv.FormatFloat(s.MaxDropPercent, 'f', -1, 64))
	case s.Lines > 0:
		summary = fmt.Sprintf("%d returned line(s) were sampled by Adaptive Logs; the dropped lines may be recoverable from the time machine", s.Lines)
	default:
		summary = "Adaptive Logs sampling dropped some matching lines; they may be recoverable from the time machine"
	}
	cmdio.EmitHint(w, summary, command)
}

// SurfaceNotices writes each datasource notice on resp to w as a hint.
func SurfaceNotices(w io.Writer, resp *loki.QueryResponse) {
	for _, notice := range resp.Data.Notices {
		cmdio.EmitHint(w, notice.Text, "")
	}
}
