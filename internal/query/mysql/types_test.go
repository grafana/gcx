package mysql_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscapeSQLString(t *testing.T) {
	assert.Equal(t, "it''s", mysql.EscapeSQLString("it's"))
	assert.Equal(t, "plain", mysql.EscapeSQLString("plain"))
}

func TestValidateIdentifier(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, name := range []string{"", "orders", "my_table_2"} {
			assert.NoError(t, mysql.ValidateIdentifier(name, "table"), name)
		}
	})

	t.Run("rejects", func(t *testing.T) {
		for _, name := range []string{"bad-name", "x; DROP TABLE y", "1table", "a'b", "a`b", "testdb.orders"} {
			err := mysql.ValidateIdentifier(name, "table")
			require.Error(t, err, name)
			assert.Contains(t, err.Error(), "invalid table")
		}
	})
}

func TestEnforceLimit(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		limit      int
		want       string
		wantCapped bool
	}{
		{"appends LIMIT when missing", "SELECT 1", 100, "SELECT 1 LIMIT 100", false},
		{"caps existing LIMIT exceeding max", "SELECT 1 LIMIT 5000", 100, "SELECT 1 LIMIT 1000", true},
		{"keeps existing LIMIT if under max", "SELECT 1 LIMIT 50", 100, "SELECT 1 LIMIT 50", false},
		{"limit 0 disables enforcement", "SELECT 1", 0, "SELECT 1", false},
		{"bail on LIMIT OFFSET", "SELECT id FROM t LIMIT 10 OFFSET 20", 100, "SELECT id FROM t LIMIT 10 OFFSET 20", false},
		{"bail on LIMIT comma syntax", "SELECT id FROM t LIMIT 10, 20", 100, "SELECT id FROM t LIMIT 10, 20", false},
		{"bail on OFFSET", "SELECT id FROM t OFFSET 20", 100, "SELECT id FROM t OFFSET 20", false},
		{"bail on INTO OUTFILE", "SELECT id FROM t INTO OUTFILE '/tmp/x'", 100, "SELECT id FROM t INTO OUTFILE '/tmp/x'", false},
		{"bail on FOR UPDATE", "SELECT id FROM t FOR UPDATE", 100, "SELECT id FROM t FOR UPDATE", false},
		{"bail on LOCK IN SHARE MODE", "SELECT id FROM t LOCK IN SHARE MODE", 100, "SELECT id FROM t LOCK IN SHARE MODE", false},
		{"bail on EXPLAIN", "EXPLAIN SELECT id FROM t", 100, "EXPLAIN SELECT id FROM t", false},
		{"bail on SHOW", "SHOW TABLES", 100, "SHOW TABLES", false},
		{"bail on DESCRIBE", "DESCRIBE orders", 100, "DESCRIBE orders", false},
		{"bail on short-form DESC", "DESC orders", 100, "DESC orders", false},
		{"UPDATE passes through", "UPDATE t SET a = 1", 100, "UPDATE t SET a = 1", false},
		{"DELETE passes through", "DELETE FROM t WHERE a = 1", 100, "DELETE FROM t WHERE a = 1", false},
		{"INSERT VALUES passes through", "INSERT INTO t (a) VALUES (1)", 100, "INSERT INTO t (a) VALUES (1)", false},
		{"INSERT SELECT passes through", "INSERT INTO t2 SELECT * FROM t1", 100, "INSERT INTO t2 SELECT * FROM t1", false},
		{"REPLACE passes through", "REPLACE INTO t (a) VALUES (1)", 100, "REPLACE INTO t (a) VALUES (1)", false},
		{"DDL passes through", "CREATE TABLE t (a int)", 100, "CREATE TABLE t (a int)", false},
		{"CTE SELECT gets LIMIT", "WITH x AS (SELECT 1) SELECT * FROM x", 100, "WITH x AS (SELECT 1) SELECT * FROM x LIMIT 100", false},
		{"CTE-wrapped UPDATE passes through", "WITH x AS (SELECT id FROM t) UPDATE t JOIN x ON t.id = x.id SET t.a = 1", 100, "WITH x AS (SELECT id FROM t) UPDATE t JOIN x ON t.id = x.id SET t.a = 1", false},
		{"SELECT mentioning DML in a literal skips enforcement", "SELECT * FROM audit WHERE action = 'DELETE'", 100, "SELECT * FROM audit WHERE action = 'DELETE'", false},
		{"subquery LIMIT appends without cap warning", "SELECT * FROM (SELECT a FROM t LIMIT 5000) x", 100, "SELECT * FROM (SELECT a FROM t LIMIT 5000) x LIMIT 100", false},
		{"multiline ORDER BY DESC still gets LIMIT", "SELECT * FROM t ORDER BY created_at\nDESC", 100, "SELECT * FROM t ORDER BY created_at\nDESC LIMIT 100", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, capped := mysql.EnforceLimit(tt.sql, tt.limit, 1000)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCapped, capped)
		})
	}
}
