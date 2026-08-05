package bigquery_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/bigquery"
	querysql "github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscapeSQLString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal name", "events", "events"},
		{"single quote", "it's", "it''s"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bigquery.EscapeSQLString(tt.input))
		})
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid", "WORLD_DATA", false},
		{"valid lowercase", "my_dataset", false},
		{"empty is ok", "", false},
		{"has hyphen (not allowed for dataset/table)", "my-dataset", true},
		{"has dot", "ds.tbl", true},
		{"has single quote", "it's", true},
		{"has backtick", "t`ble", true},
		{"has space", "my table", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bigquery.ValidateName(tt.input, "dataset")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateProject(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid with hyphen", "partner-datasources", false},
		{"valid plain", "myproject", false},
		{"empty is ok", "", false},
		{"has dot", "my.project", true},
		{"has single quote", "proj'; DROP", true},
		{"has backtick", "pro`ject", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bigquery.ValidateProject(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSplitQualifiedTable(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantProject string
		wantDataset string
		wantTable   string
		wantErr     bool
	}{
		{"bare table", "WORLD_DATA", "", "", "WORLD_DATA", false},
		{"dataset qualified", "my_dataset.WORLD_DATA", "", "my_dataset", "WORLD_DATA", false},
		{"project qualified", "my-project.my_dataset.WORLD_DATA", "my-project", "my_dataset", "WORLD_DATA", false},
		{"four parts errors", "a.b.c.d", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project, dataset, table, err := bigquery.SplitQualifiedTable(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantProject, project)
			assert.Equal(t, tt.wantDataset, dataset)
			assert.Equal(t, tt.wantTable, table)
		})
	}
}

func TestInfoSchemaPrefix(t *testing.T) {
	tests := []struct {
		name    string
		project string
		dataset string
		want    string
	}{
		{"project and dataset", "partner-datasources", "grafana_example_dataset", "`partner-datasources.grafana_example_dataset`.INFORMATION_SCHEMA"},
		{"dataset only", "", "my_dataset", "`my_dataset`.INFORMATION_SCHEMA"},
		{"project only", "my-project", "", "`my-project`.INFORMATION_SCHEMA"},
		{"neither", "", "", "INFORMATION_SCHEMA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bigquery.InfoSchemaPrefix(tt.project, tt.dataset))
		})
	}
}

func TestEnforceLimit(t *testing.T) {
	tests := []struct {
		name  string
		sql   string
		limit int
		want  string
	}{
		{"appends LIMIT when missing", "SELECT 1", 100, "SELECT 1 LIMIT 100"},
		{"appends LIMIT with trailing semicolon", "SELECT 1;", 100, "SELECT 1 LIMIT 100;"},
		{"keeps existing LIMIT if under max", "SELECT 1 LIMIT 50", 100, "SELECT 1 LIMIT 50"},
		{"caps existing LIMIT exceeding max", "SELECT 1 LIMIT 5000", 1000, "SELECT 1 LIMIT 1000"},
		{"limit 0 disables enforcement", "SELECT 1", 0, "SELECT 1"},
		{"bail on LIMIT OFFSET", "SELECT * FROM t LIMIT 100 OFFSET 10", 100, "SELECT * FROM t LIMIT 100 OFFSET 10"},
		{"bail on EXPLAIN", "EXPLAIN SELECT * FROM t", 100, "EXPLAIN SELECT * FROM t"},
		{"bail on DDL CREATE", "CREATE TABLE t AS SELECT 1", 100, "CREATE TABLE t AS SELECT 1"},
		{"bail on leading DESCRIBE", "DESCRIBE mydataset.mytable", 100, "DESCRIBE mydataset.mytable"},
		{"bail on leading DESC", "DESC mydataset.mytable", 100, "DESC mydataset.mytable"},
		{"bail on leading whitespace before DESCRIBE", "\n  DESCRIBE mydataset.mytable", 100, "\n  DESCRIBE mydataset.mytable"},
		{"bail on lowercase leading describe", "describe mydataset.mytable", 100, "describe mydataset.mytable"},
		// Statement-leading keywords must not match mid-statement line starts.
		// Before the \A anchor these multi-line queries silently lost their cap.
		{
			"appends LIMIT to multi-line ORDER BY DESC",
			"SELECT * FROM `p.d.events`\nORDER BY ts\nDESC",
			100,
			"SELECT * FROM `p.d.events`\nORDER BY ts\nDESC LIMIT 100",
		},
		{
			"appends LIMIT when DESC ends a line",
			"SELECT * FROM t\nORDER BY ts DESC",
			100,
			"SELECT * FROM t\nORDER BY ts DESC LIMIT 100",
		},
		{
			"appends LIMIT to multi-line window function with DESC on its own line",
			"SELECT ROW_NUMBER() OVER (\n  ORDER BY ts\n  DESC\n) AS rn FROM t",
			100,
			"SELECT ROW_NUMBER() OVER (\n  ORDER BY ts\n  DESC\n) AS rn FROM t LIMIT 100",
		},
		{
			"bail on LIMIT OFFSET remains unanchored across lines",
			"SELECT * FROM t\nLIMIT 100 OFFSET 10",
			100,
			"SELECT * FROM t\nLIMIT 100 OFFSET 10",
		},
		// DML, scripting and remaining DDL statements: appending a LIMIT makes
		// a correct statement fail with a syntax error.
		{"bail on DML INSERT", "INSERT INTO t (a) VALUES (1)", 100, "INSERT INTO t (a) VALUES (1)"},
		{"bail on DML UPDATE", "UPDATE t SET a = 1 WHERE b = 2", 100, "UPDATE t SET a = 1 WHERE b = 2"},
		{"bail on DML DELETE", "DELETE FROM t WHERE x = 1", 100, "DELETE FROM t WHERE x = 1"},
		{"bail on DML MERGE", "MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET a = s.a", 100, "MERGE INTO t USING s ON t.id = s.id WHEN MATCHED THEN UPDATE SET a = s.a"},
		{"bail on DDL TRUNCATE", "TRUNCATE TABLE t", 100, "TRUNCATE TABLE t"},
		{"bail on scripting BEGIN", "BEGIN SELECT 1; END", 100, "BEGIN SELECT 1; END"},
		{"bail on scripting DECLARE", "DECLARE x INT64 DEFAULT 1", 100, "DECLARE x INT64 DEFAULT 1"},
		{"bail on procedure CALL", "CALL mydataset.myprocedure(1)", 100, "CALL mydataset.myprocedure(1)"},
		{"bail on lowercase DML delete", "delete from t where x = 1", 100, "delete from t where x = 1"},
		{"bail on DROP", "DROP TABLE t", 100, "DROP TABLE t"},
		{"bail on ALTER", "ALTER TABLE t ADD COLUMN a INT64", 100, "ALTER TABLE t ADD COLUMN a INT64"},
		{"bail on SHOW", "SHOW TABLES", 100, "SHOW TABLES"},
		// DML keywords must stay anchored: they are legal mid-query identifiers
		// and clause words, so they must not suppress the row cap.
		{
			"appends LIMIT when DELETE appears mid-query",
			"SELECT deleted_at\nFROM t\nWHERE action = 'delete'",
			100,
			"SELECT deleted_at\nFROM t\nWHERE action = 'delete' LIMIT 100",
		},
		{
			"appends LIMIT when UPDATE appears on a later line",
			"SELECT a\nFROM t\nORDER BY updated_at",
			100,
			"SELECT a\nFROM t\nORDER BY updated_at LIMIT 100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, bigquery.EnforceLimit(tt.sql, tt.limit, 1000))
		})
	}
}

func TestParseStringColumn(t *testing.T) {
	resp := &querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "schema_name", Type: "string"}},
		Rows:    [][]any{{"GCS"}, {"grafana_example_dataset"}},
	}
	assert.Equal(t, []string{"GCS", "grafana_example_dataset"}, bigquery.ParseStringColumn(resp))
}

func TestParseTableInfoRows(t *testing.T) {
	resp := &querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "table_name"}, {Name: "table_type"}},
		Rows:    [][]any{{"WORLD_DATA", "BASE TABLE"}, {"short"}},
	}
	got := bigquery.ParseTableInfoRows(resp)
	// The short row (< 2 columns) is skipped.
	assert.Equal(t, []bigquery.TableInfo{{Name: "WORLD_DATA", Type: "BASE TABLE"}}, got)
}

func TestParseColumnInfoRows(t *testing.T) {
	resp := &querysql.QueryResponse{
		Columns: []querysql.Column{{Name: "column_name"}, {Name: "data_type"}, {Name: "is_nullable"}},
		Rows:    [][]any{{"birth_rate", "INT64", "YES"}, {"country_name", "STRING", "NO"}},
	}
	got := bigquery.ParseColumnInfoRows(resp)
	assert.Equal(t, []bigquery.ColumnInfo{
		{Name: "birth_rate", Type: "INT64", Nullable: "YES"},
		{Name: "country_name", Type: "STRING", Nullable: "NO"},
	}, got)
}
