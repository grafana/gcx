package query

import (
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
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
