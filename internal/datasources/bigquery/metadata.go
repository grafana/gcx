package bigquery

import (
	"fmt"
	"io"

	cmdio "github.com/grafana/gcx/internal/output"
	querysql "github.com/grafana/gcx/internal/query/sql"
)

// warnIfMetadataTruncated drops rows beyond limit (the caller must request
// limit+1 rows, which bigquery.MetadataQuery does) and warns on warnw that more
// rows matched, so a capped metadata listing never reads as the complete
// inventory. A limit of 0 means unbounded: nothing is dropped and nothing is
// warned about. Shared by list-datasets, list-tables, and describe-table, which
// all cap the same INFORMATION_SCHEMA shape the same way.
func warnIfMetadataTruncated(warnw io.Writer, resp *querysql.QueryResponse, noun string, limit int) {
	if limit <= 0 || len(resp.Rows) <= limit {
		return
	}
	resp.Rows = resp.Rows[:limit]
	cmdio.Warning(warnw,
		"showing the first %d %s; more %s match — narrow with --filter, or raise --limit (--limit 0 for no cap)",
		limit, noun, noun)
}

// validateMetadataLimit rejects a negative --limit. Zero is valid and means
// unbounded, matching the `--limit 0` convention of 'gcx datasources bigquery
// query'.
func validateMetadataLimit(limit int) error {
	if limit < 0 {
		return fmt.Errorf("--limit must be >= 0, got %d", limit)
	}
	return nil
}
