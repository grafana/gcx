package postgres_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscapeSQLString(t *testing.T) {
	assert.Equal(t, "it''s", postgres.EscapeSQLString("it's"))
	assert.Equal(t, "plain", postgres.EscapeSQLString("plain"))
}

func TestValidateIdentifier(t *testing.T) {
	t.Run("accepts", func(t *testing.T) {
		for _, name := range []string{"", "orders", "my_table_2"} {
			assert.NoError(t, postgres.ValidateIdentifier(name, "table"), name)
		}
	})

	t.Run("rejects", func(t *testing.T) {
		for _, name := range []string{"bad-name", "x; DROP TABLE y", "1table", "a'b", "public.orders"} {
			err := postgres.ValidateIdentifier(name, "table")
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
		{"bail on OFFSET", "SELECT id FROM t OFFSET 20", 100, "SELECT id FROM t OFFSET 20", false},
		{"bail on FETCH FIRST", "SELECT id FROM t FETCH FIRST 10 ROWS ONLY", 100, "SELECT id FROM t FETCH FIRST 10 ROWS ONLY", false},
		{"bail on RETURNING", "UPDATE t SET a = 1 RETURNING id", 100, "UPDATE t SET a = 1 RETURNING id", false},
		{"bail on FOR UPDATE", "SELECT id FROM t FOR UPDATE", 100, "SELECT id FROM t FOR UPDATE", false},
		{"bail on EXPLAIN", "EXPLAIN SELECT id FROM t", 100, "EXPLAIN SELECT id FROM t", false},
		{"bail on SHOW", "SHOW search_path", 100, "SHOW search_path", false},
		{"UPDATE passes through", "UPDATE t SET a = 1", 100, "UPDATE t SET a = 1", false},
		{"DELETE passes through", "DELETE FROM t WHERE a = 1", 100, "DELETE FROM t WHERE a = 1", false},
		{"INSERT passes through", "INSERT INTO t (a) VALUES (1)", 100, "INSERT INTO t (a) VALUES (1)", false},
		{"DDL passes through", "CREATE TABLE t (a int)", 100, "CREATE TABLE t (a int)", false},
		{"CTE SELECT gets LIMIT", "WITH x AS (SELECT 1) SELECT * FROM x", 100, "WITH x AS (SELECT 1) SELECT * FROM x LIMIT 100", false},
		{"CTE-wrapped DELETE passes through", "WITH victims AS (SELECT id FROM t) DELETE FROM t USING victims WHERE t.id = victims.id", 100, "WITH victims AS (SELECT id FROM t) DELETE FROM t USING victims WHERE t.id = victims.id", false},
		{"SELECT mentioning DML in a literal skips enforcement", "SELECT * FROM audit WHERE action = 'DELETE'", 100, "SELECT * FROM audit WHERE action = 'DELETE'", false},
		{"subquery LIMIT appends without cap warning", "SELECT * FROM (SELECT a FROM t LIMIT 5000) x", 100, "SELECT * FROM (SELECT a FROM t LIMIT 5000) x LIMIT 100", false},
		{"DML-like column names do not bail", "SELECT last_update, deleted_at FROM t", 100, "SELECT last_update, deleted_at FROM t LIMIT 100", false},
		{"multiline query with a line starting in a keyword-like name still gets LIMIT", "SELECT id,\nshow FROM schedule", 100, "SELECT id,\nshow FROM schedule LIMIT 100", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, capped := postgres.EnforceLimit(tt.sql, tt.limit, 1000)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCapped, capped)
		})
	}
}
