package bigquery

import (
	"fmt"
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	querysql "github.com/grafana/gcx/internal/query/sql"
)

// warnIfMetadataTruncated drops rows beyond limit (the caller must request
// limit+1 rows, which bigquery.MetadataQuery does) and reports on warnw that
// more rows matched, so a capped metadata listing never reads as the complete
// inventory. A limit of 0 means unbounded: nothing is dropped and nothing is
// reported. Shared by list-datasets, list-tables, and describe-table, which all
// cap the same INFORMATION_SCHEMA shape the same way.
//
// The notice goes out as a typed warn plus a hint carrying the continuation
// flags, rather than as cmdio.Warning's prose line, so agent-mode stderr stays
// JSONL like the shared dsquery.SurfaceRowLimits path the other SQL
// datasources use.
func warnIfMetadataTruncated(warnw io.Writer, resp *querysql.QueryResponse, noun string, limit int) {
	if limit <= 0 || len(resp.Rows) <= limit {
		return
	}
	resp.Rows = resp.Rows[:limit]
	cmdio.EmitWarn(warnw, fmt.Sprintf("showing the first %d %s; more %s match", limit, noun, noun))
	cmdio.EmitHint(warnw, "narrow the listing, or remove the cap", "--filter SUBSTRING / --limit 0")
}
