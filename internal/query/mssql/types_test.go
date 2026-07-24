package mssql_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/mssql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforceTop(t *testing.T) {
	tests := []struct {
		name  string
		sql   string
		limit int
		want  string
	}{
		{"simple select", "SELECT * FROM dbo.t", 100, "SELECT TOP (100) * FROM dbo.t"},
		{"select distinct", "SELECT DISTINCT name FROM dbo.t", 10, "SELECT DISTINCT TOP (10) name FROM dbo.t"},
		{"select all", "SELECT ALL name FROM dbo.t", 5, "SELECT ALL TOP (5) name FROM dbo.t"},
		{"lowercase select", "select id from dbo.t", 3, "select TOP (3) id from dbo.t"},
		{"leading whitespace", "  \n SELECT id FROM dbo.t", 7, "  \n SELECT TOP (7) id FROM dbo.t"},
		{"clamped to max", "SELECT * FROM dbo.t", 99999, "SELECT TOP (1000) * FROM dbo.t"},
		{"limit zero disables", "SELECT * FROM dbo.t", 0, "SELECT * FROM dbo.t"},
		{"existing top untouched", "SELECT TOP 5 * FROM dbo.t", 100, "SELECT TOP 5 * FROM dbo.t"},
		{"existing top parens untouched", "SELECT TOP (5) * FROM dbo.t", 100, "SELECT TOP (5) * FROM dbo.t"},
		{"cte bails", "WITH c AS (SELECT 1 AS n) SELECT * FROM c", 100, "WITH c AS (SELECT 1 AS n) SELECT * FROM c"},
		{"union bails", "SELECT a FROM t1 UNION SELECT a FROM t2", 100, "SELECT a FROM t1 UNION SELECT a FROM t2"},
		{"offset fetch bails", "SELECT * FROM dbo.t ORDER BY id OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY", 100, "SELECT * FROM dbo.t ORDER BY id OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY"},
		{"non-select bails", "EXEC sp_who", 100, "EXEC sp_who"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, mssql.EnforceTop(tt.sql, tt.limit, 1000))
		})
	}
}

func TestEscapeSQLString(t *testing.T) {
	assert.Equal(t, "dbo", mssql.EscapeSQLString("dbo"))
	assert.Equal(t, "O''Brien", mssql.EscapeSQLString("O'Brien"))
	assert.Equal(t, "a''b''c", mssql.EscapeSQLString("a'b'c"))
}

func TestValidateIdentifier(t *testing.T) {
	require.NoError(t, mssql.ValidateIdentifier("", "schema"))
	require.NoError(t, mssql.ValidateIdentifier("dbo", "schema"))
	require.NoError(t, mssql.ValidateIdentifier("WORLD_DATA", "table"))
	require.Error(t, mssql.ValidateIdentifier("bad name", "table"))
	require.Error(t, mssql.ValidateIdentifier("schema.table", "table"))
	require.Error(t, mssql.ValidateIdentifier("1table", "table"))
	require.Error(t, mssql.ValidateIdentifier("drop;--", "table"))
}
