package pinot_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTableName(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"simple from", `SELECT count(*) FROM events`, "events"},
		{"quoted from", `SELECT 1 FROM "events"`, "events"},
		{"set prefix", "SET useMultistageEngine = true;\nSELECT * FROM logs", "logs"},
		{"schema-qualified", "SELECT * FROM my_db.events", "my_db.events"},
		{"quoted schema-qualified", `SELECT * FROM "db"."events"`, "db.events"},
		{"quoted schema unquoted table", `SELECT * FROM "db".events`, "db.events"},
		{"unquoted schema quoted table", `SELECT * FROM db."events"`, "db.events"},
		{"quoted three-part name", `SELECT * FROM "cat"."db"."events"`, "cat.db.events"},
		{"quoted schema with spaces around dots", `SELECT * FROM "db" . "events"`, "db.events"},
		{"subquery from skipped", "SELECT * FROM (SELECT 1)", ""},
		{"subquery uses inner table", "SELECT * FROM (SELECT x FROM inner_t) a", "inner_t"},
		{"subquery count from events", "SELECT count(*) FROM (SELECT col FROM events) sub", "events"},
		{"union wrapper uses first inner table", "SELECT * FROM (\n  SELECT 1 FROM measurements\n  UNION ALL\n  SELECT 1 FROM events\n) journey", "measurements"},
		{"extract year from is not the table", `SELECT EXTRACT(YEAR FROM ts) FROM events`, "events"},
		{"from inside string is not the table", `SELECT 'FROM x' AS a FROM events`, "events"},
		{"from inside string lowercase", `SELECT 'from admin' AS x FROM t`, "t"},
		{"hyphenated quoted table", `SELECT * FROM "my-table"`, "my-table"},
		{"table alias", `SELECT * FROM events AS e`, "events"},
		{"join uses left table", `SELECT * FROM orders AS o JOIN customers AS c ON o.id = c.id`, "orders"},
		{"no from", "SELECT 1", ""},
		{"trailing dot after quoted schema", `SELECT * FROM "db".`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pinot.ExtractTableName(tt.sql))
		})
	}
}

func TestResolveTableName(t *testing.T) {
	t.Run("override wins", func(t *testing.T) {
		got, err := pinot.ResolveTableName("SELECT 1", "events")
		require.NoError(t, err)
		assert.Equal(t, "events", got)
	})
	t.Run("override wins over extracted", func(t *testing.T) {
		got, err := pinot.ResolveTableName("SELECT 1 FROM logs", "events")
		require.NoError(t, err)
		assert.Equal(t, "events", got)
	})
	t.Run("blank override falls back to extract", func(t *testing.T) {
		got, err := pinot.ResolveTableName("SELECT 1 FROM logs", "  ")
		require.NoError(t, err)
		assert.Equal(t, "logs", got)
	})
	t.Run("empty when neither override nor from", func(t *testing.T) {
		got, err := pinot.ResolveTableName("SELECT 1", "")
		require.ErrorIs(t, err, pinot.ErrTableNameRequired)
		assert.Empty(t, got)
		assert.Contains(t, err.Error(), "--table")
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
		{"appends LIMIT after SET prefix", "SET useMultistageEngine = true;\nSELECT 1", 100, "SET useMultistageEngine = true;\nSELECT 1 LIMIT 100", false},
		{"keeps existing LIMIT if under max", "SELECT 1 LIMIT 50", 100, "SELECT 1 LIMIT 50", false},
		{"caps existing LIMIT exceeding max", "SELECT 1 LIMIT 5000", 100, "SELECT 1 LIMIT 1000", true},
		{"requested limit above max is capped and reported", "SELECT 1", 5000, "SELECT 1 LIMIT 1000", true},
		{"limit 0 disables enforcement", "SELECT 1", 0, "SELECT 1", false},
		{"bail on UNION", "SELECT 1 FROM a UNION SELECT 2 FROM b LIMIT 2000", 100, "SELECT 1 FROM a UNION SELECT 2 FROM b LIMIT 2000", false},
		{"bail on SET plus UNION", "SET useMultistageEngine = true;\nSELECT 1 FROM a\nUNION ALL\nSELECT 2 FROM b LIMIT 2000", 100, "SET useMultistageEngine = true;\nSELECT 1 FROM a\nUNION ALL\nSELECT 2 FROM b LIMIT 2000", false},
		{"bail on LIMIT OFFSET", "SELECT * FROM t LIMIT 100 OFFSET 10", 100, "SELECT * FROM t LIMIT 100 OFFSET 10", false},
		{"keeps LIMIT offset,count as written", "SELECT * FROM t LIMIT 10, 20", 100, "SELECT * FROM t LIMIT 10, 20", false},
		{"leaves OPTION unchanged", "SELECT * FROM t OPTION(timeoutMs=5000)", 100, "SELECT * FROM t OPTION(timeoutMs=5000)", false},
		{"leaves LIMIT before OPTION unchanged", "SELECT * FROM t LIMIT 5000 OPTION(timeoutMs=5000)", 100, "SELECT * FROM t LIMIT 5000 OPTION(timeoutMs=5000)", false},
		{"explain is not SELECT or WITH", "EXPLAIN SELECT * FROM t", 100, "EXPLAIN SELECT * FROM t", false},
		{"insert is not SELECT or WITH", "INSERT INTO t VALUES (1)", 100, "INSERT INTO t VALUES (1)", false},
		{"bail on trailing line comment", "SELECT 1 -- keep going", 100, "SELECT 1 -- keep going", false},
		{"appends LIMIT when delete is only a literal", "SELECT * FROM t WHERE action = 'delete'", 100, "SELECT * FROM t WHERE action = 'delete' LIMIT 100", false},
		{"appends LIMIT when -- is only a literal", "SELECT '--' FROM t", 100, "SELECT '--' FROM t LIMIT 100", false},
		{
			"appends LIMIT to multi-line SELECT",
			"SELECT * FROM t\nORDER BY ts DESC",
			100,
			"SELECT * FROM t\nORDER BY ts DESC LIMIT 100",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, capped := pinot.EnforceLimit(tt.sql, tt.limit, 1000)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCapped, capped)
		})
	}
}

func TestLimitNotEnforced(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{"plain select", "SELECT 1", false},
		{"plain select with limit", "SELECT 1 LIMIT 50", false},
		{"limit offset,count already bounds rows", "SELECT * FROM t LIMIT 10, 20", false},
		{"option cannot take a trailing limit", "SELECT * FROM t OPTION(timeoutMs=5000)", true},
		{"limit then option cannot be rewritten", "SELECT * FROM t LIMIT 5000 OPTION(timeoutMs=5000)", true},
		{"union", "SELECT 1 FROM a UNION SELECT 2 FROM b LIMIT 2000", true},
		{"union all after SET", "SET useMultistageEngine = true;\nSELECT 1 FROM a\nUNION ALL\nSELECT 2 FROM b LIMIT 2000", true},
		{"limit offset", "SELECT * FROM t LIMIT 5000 OFFSET 0", true},
		{"bare offset", "SELECT * FROM t OFFSET 10", true},
		{"explain never reaches bail", "EXPLAIN SELECT * FROM t", false},
		{"insert never reaches bail", "INSERT INTO t VALUES (1)", false},
		{"trailing comment skips enforcement", "SELECT 1 -- keep going", true},
		{"dml word in literal is not union/offset", "SELECT * FROM t WHERE action = 'delete'", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pinot.LimitNotEnforced(tt.sql))
		})
	}
}
