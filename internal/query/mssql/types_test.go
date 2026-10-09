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
		{"negative limit disables", "SELECT * FROM dbo.t", -5, "SELECT * FROM dbo.t"},
		{"existing top untouched", "SELECT TOP 5 * FROM dbo.t", 100, "SELECT TOP 5 * FROM dbo.t"},
		{"existing top parens untouched", "SELECT TOP (5) * FROM dbo.t", 100, "SELECT TOP (5) * FROM dbo.t"},
		{"existing top percent not clamped", "SELECT TOP 50 PERCENT * FROM dbo.t", 100, "SELECT TOP 50 PERCENT * FROM dbo.t"},
		{"existing top with ties not clamped", "SELECT TOP (5) WITH TIES * FROM dbo.t ORDER BY id", 100, "SELECT TOP (5) WITH TIES * FROM dbo.t ORDER BY id"},
		{"cte bails", "WITH c AS (SELECT 1 AS n) SELECT * FROM c", 100, "WITH c AS (SELECT 1 AS n) SELECT * FROM c"},
		{"union bails", "SELECT a FROM t1 UNION SELECT a FROM t2", 100, "SELECT a FROM t1 UNION SELECT a FROM t2"},
		{"offset fetch bails", "SELECT * FROM dbo.t ORDER BY id OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY", 100, "SELECT * FROM dbo.t ORDER BY id OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY"},
		{"non-select bails", "EXEC sp_who", 100, "EXEC sp_who"},
		{"select into bails", "SELECT * INTO dbo.backup FROM dbo.orders", 100, "SELECT * INTO dbo.backup FROM dbo.orders"},
		{"select into with distinct bails", "SELECT DISTINCT name INTO #tmp FROM dbo.t", 100, "SELECT DISTINCT name INTO #tmp FROM dbo.t"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A byte-identical assertion, not just "changed" vs "unchanged": a
			// bail that accidentally still mutated whitespace or case would
			// pass a looser check but silently alter the write target's SQL.
			got := mssql.EnforceTop(tt.sql, tt.limit, 1000)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnforceTopSentinel(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		limit      int
		wantSQL    string
		wantEff    int
		wantCapped bool
	}{
		{"injects eff+1 sentinel", "SELECT * FROM dbo.t", 100, "SELECT TOP (101) * FROM dbo.t", 100, true},
		{"sentinel at the ceiling detects >max", "SELECT * FROM dbo.t", 1000, "SELECT TOP (1001) * FROM dbo.t", 1000, true},
		{"above ceiling clamps eff, still sentinels", "SELECT * FROM dbo.t", 99999, "SELECT TOP (1001) * FROM dbo.t", 1000, true},
		{"limit zero disables", "SELECT * FROM dbo.t", 0, "SELECT * FROM dbo.t", 0, false},
		{"negative limit disables", "SELECT * FROM dbo.t", -5, "SELECT * FROM dbo.t", 0, false},
		{"existing top not sentineled", "SELECT TOP (5) * FROM dbo.t", 100, "SELECT TOP (5) * FROM dbo.t", 100, false},
		{"cte bails", "WITH c AS (SELECT 1 AS n) SELECT * FROM c", 100, "WITH c AS (SELECT 1 AS n) SELECT * FROM c", 100, false},
		{"offset fetch bails", "SELECT * FROM dbo.t ORDER BY id OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY", 100, "SELECT * FROM dbo.t ORDER BY id OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY", 100, false},
		{"select into bails", "SELECT * INTO dbo.backup FROM dbo.orders", 100, "SELECT * INTO dbo.backup FROM dbo.orders", 100, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotEff, gotCapped := mssql.EnforceTopSentinel(tt.sql, tt.limit, 1000)
			assert.Equal(t, tt.wantSQL, gotSQL)
			assert.Equal(t, tt.wantEff, gotEff)
			assert.Equal(t, tt.wantCapped, gotCapped)
		})
	}
}

func TestEscapeSQLString(t *testing.T) {
	assert.Equal(t, "dbo", mssql.EscapeSQLString("dbo"))
	assert.Equal(t, "O''Brien", mssql.EscapeSQLString("O'Brien"))
	assert.Equal(t, "a''b''c", mssql.EscapeSQLString("a'b'c"))
}

func TestSplitSchemaQualifiedTable(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantSchema string
		wantTable  string
		wantErr    bool
	}{
		{"bare table", "WORLD_DATA", "", "WORLD_DATA", false},
		{"schema qualified", "dbo.WORLD_DATA", "dbo", "WORLD_DATA", false},
		{"three parts errors", "db.dbo.WORLD_DATA", "", "", true},
		{"trailing dot errors", "dbo.", "", "", true},
		{"leading dot errors", ".WORLD_DATA", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema, table, err := mssql.SplitSchemaQualifiedTable(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSchema, schema)
			assert.Equal(t, tt.wantTable, table)
		})
	}
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
