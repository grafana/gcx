package bigquery

import (
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	"github.com/grafana/gcx/internal/query/bigquery"
	querysql "github.com/grafana/gcx/internal/query/sql"
)

// warnIfMetadataTruncated drops rows beyond bigquery.MetadataRowLimit (the
// caller must request MetadataRowLimit+1 rows via its own LIMIT clause) and
// warns on warnw that more rows matched, so a capped metadata listing never
// reads as the complete inventory. Shared by list-datasets, list-tables, and
// describe-table, which all cap the same INFORMATION_SCHEMA shape the same way.
func warnIfMetadataTruncated(warnw io.Writer, resp *querysql.QueryResponse, noun string) {
	if len(resp.Rows) <= bigquery.MetadataRowLimit {
		return
	}
	resp.Rows = resp.Rows[:bigquery.MetadataRowLimit]
	cmdio.Warning(warnw, "showing the first %d %s; more %s match", bigquery.MetadataRowLimit, noun, noun)
}
