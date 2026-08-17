package postgres

import (
	"bytes"
	"testing"

	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
)

func TestBuildListTablesSQL(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		rowCap int
		want   string
	}{
		{
			name:   "no schema filter requests rowCap+1 rows",
			schema: "",
			rowCap: 500,
			want:   "SELECT table_schema AS schema, table_name AS name, table_type AS type FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema') ORDER BY table_schema, table_name LIMIT 501",
		},
		{
			name:   "schema filter is appended and escaped",
			schema: "public",
			rowCap: 500,
			want:   "SELECT table_schema AS schema, table_name AS name, table_type AS type FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema = 'public' ORDER BY table_schema, table_name LIMIT 501",
		},
		{
			name:   "single quotes in schema are escaped",
			schema: "o'brien",
			rowCap: 500,
			want:   "SELECT table_schema AS schema, table_name AS name, table_type AS type FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema = 'o''brien' ORDER BY table_schema, table_name LIMIT 501",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildListTablesSQL(tt.schema, tt.rowCap))
		})
	}
}

func rowsOfLen(n int) [][]any {
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{"public", "t", "BASE TABLE"}
	}
	return rows
}

func TestWarnIfTruncated(t *testing.T) {
	t.Run("under cap: no truncation, no warning", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rowsOfLen(500)}
		var stderr bytes.Buffer
		warnIfTruncated(&stderr, resp, 500)
		assert.Len(t, resp.Rows, 500)
		assert.Empty(t, stderr.String())
	})

	t.Run("over cap: rows dropped to cap and warning emitted", func(t *testing.T) {
		resp := &querysql.QueryResponse{Rows: rowsOfLen(501)}
		var stderr bytes.Buffer
		warnIfTruncated(&stderr, resp, 500)
		assert.Len(t, resp.Rows, 500)
		assert.Contains(t, stderr.String(), "showing the first 500 tables")
		assert.Contains(t, stderr.String(), "--schema")
	})
}
