package mysql

import (
	"bytes"
	"testing"

	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
)

func TestBuildListTablesSQL(t *testing.T) {
	tests := []struct {
		name     string
		database string
		rowCap   int
		want     string
	}{
		{
			name:     "no database filter requests rowCap+1 rows",
			database: "",
			rowCap:   500,
			want:     "SELECT table_schema AS `database`, table_name AS name, table_type AS type FROM information_schema.tables WHERE table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys') ORDER BY table_schema, table_name LIMIT 501",
		},
		{
			name:     "database filter is appended and escaped",
			database: "grafanadb",
			rowCap:   500,
			want:     "SELECT table_schema AS `database`, table_name AS name, table_type AS type FROM information_schema.tables WHERE table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys') AND table_schema = 'grafanadb' ORDER BY table_schema, table_name LIMIT 501",
		},
		{
			name:     "single quotes in database are escaped",
			database: "o'brien",
			rowCap:   500,
			want:     "SELECT table_schema AS `database`, table_name AS name, table_type AS type FROM information_schema.tables WHERE table_schema NOT IN ('mysql', 'information_schema', 'performance_schema', 'sys') AND table_schema = 'o''brien' ORDER BY table_schema, table_name LIMIT 501",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildListTablesSQL(tt.database, tt.rowCap))
		})
	}
}

func rowsOfLen(n int) [][]any {
	rows := make([][]any, n)
	for i := range rows {
		rows[i] = []any{"grafanadb", "t", "BASE TABLE"}
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
		assert.Contains(t, stderr.String(), "--database")
	})
}
