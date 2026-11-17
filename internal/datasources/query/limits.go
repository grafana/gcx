package query

import (
	"fmt"
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/loki"
	querysql "github.com/grafana/gcx/internal/query/sql"
)

const (
	// DefaultLokiLimit is the default result cap for Loki queries when --limit
	// is not explicitly provided. A smaller value avoids overwhelming output;
	// use --limit 0 for the backend default or --limit N for a custom value.
	DefaultLokiLimit = 50

	// LokiZeroLimitNotice is the stderr hint for --limit 0. Scripted callers
	// never read help text, and the backend cap can cut results short silently.
	LokiZeroLimitNotice = "--limit 0 uses the Loki backend default limit, so results may be incomplete. Use a LogQL metric query for counts."
)

// ValidateLimit rejects a negative --limit. Zero is valid: gcx sends no cap.
func ValidateLimit(limit int) error {
	if limit < 0 {
		return fmt.Errorf("--limit must be >= 0, got %d", limit)
	}
	return nil
}

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

// SurfaceLogLimit hints when a log result may be incomplete. With --limit 0 the
// backend applies its own cap; a full --limit page may have more lines. The
// response cannot tell either case from a complete result. Call it only after
// loki.Client.Query succeeds: Query rejects metric LogQL, where line limits do
// not apply.
func SurfaceLogLimit(w io.Writer, resp *loki.QueryResponse, limit int) {
	count := 0
	for _, stream := range resp.Data.Result {
		count += len(stream.Values)
	}
	switch {
	case limit == 0 && count > 0:
		cmdio.EmitHint(w, LokiZeroLimitNotice, "")
	case limit > 0 && count >= limit:
		cmdio.EmitHint(w, fmt.Sprintf("query reached --limit %d; results may be incomplete. Increase --limit to retrieve more log lines", limit), "")
	}
}
