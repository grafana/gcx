package bigquery

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/bigquery"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
)

func rowsOfLen(n int) [][]any {
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{"x"}
	}
	return rows
}

// TestWarnIfMetadataTruncated pins the exact boundary: exactly at
// bigquery.MetadataRowLimit must not warn or trim (an off-by-one here — <=
// flipped to < — would trim a complete result), and one row past it must
// trim back to the cap and warn (dropping the trim would leave the extra
// sentinel row in the response).
func TestWarnIfMetadataTruncated(t *testing.T) {
	t.Run("exactly at cap: no truncation, no warning", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rowsOfLen(bigquery.MetadataRowLimit)}
		var stderr bytes.Buffer
		warnIfMetadataTruncated(&stderr, resp, "tables")
		assert.Len(t, resp.Rows, bigquery.MetadataRowLimit)
		assert.Empty(t, stderr.String())
	})

	t.Run("one over cap: rows dropped to cap and warning emitted", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rowsOfLen(bigquery.MetadataRowLimit + 1)}
		var stderr bytes.Buffer
		warnIfMetadataTruncated(&stderr, resp, "tables")
		assert.Len(t, resp.Rows, bigquery.MetadataRowLimit)
		assert.Contains(t, stderr.String(), "showing the first 1000 tables")
		assert.Contains(t, stderr.String(), "more tables match")
	})

	t.Run("under cap: unaffected", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rowsOfLen(3)}
		var stderr bytes.Buffer
		warnIfMetadataTruncated(&stderr, resp, "datasets")
		assert.Len(t, resp.Rows, 3)
		assert.Empty(t, stderr.String())
	})

	t.Run("noun is threaded into the warning", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rowsOfLen(bigquery.MetadataRowLimit + 1)}
		var stderr bytes.Buffer
		warnIfMetadataTruncated(&stderr, resp, "columns")
		assert.Contains(t, stderr.String(), "showing the first 1000 columns")
		assert.Contains(t, stderr.String(), "more columns match")
	})
}
