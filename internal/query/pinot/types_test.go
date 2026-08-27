package pinot_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/pinot"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEscapeSQLString(t *testing.T) {
	assert.Equal(t, "events", pinot.EscapeSQLString("events"))
	assert.Equal(t, "it''s", pinot.EscapeSQLString("it's"))
	assert.Empty(t, pinot.EscapeSQLString(""))
}

func TestFormatSQLInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "decimal", input: "66", want: "66"},
		{name: "canonicalizes leading zeros", input: "066", want: "66"},
		{name: "zero", input: "0", want: "0"},
		{name: "negative", input: "-1", want: "-1"},
		{name: "rejects injection", input: "66; DROP TABLE events", wantErr: true},
		{name: "rejects or-clause", input: "66 OR 1=1", wantErr: true},
		{name: "rejects quoted payload", input: "1' OR '1'='1", wantErr: true},
		{name: "rejects empty", input: "", wantErr: true},
		{name: "rejects letters", input: "abc", wantErr: true},
		{name: "rejects whitespace", input: " 66 ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pinot.FormatSQLInt(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, got)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}


func TestExtractTableName(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{"simple table", "SELECT * FROM events", "events"},
		{"schema qualified", "SELECT * FROM db.events", "db.events"},
		{"quoted schema.table", `SELECT * FROM "db"."events"`, "db.events"},
		{"quoted table with escaped quote", `SELECT * FROM "my""table"`, `my"table`},
		{"quoted schema.table with escaped quote", `SELECT * FROM "db"."my""table"`, `db.my"table`},
		{"subquery inner table", "SELECT count(*) FROM (SELECT col FROM events) sub", "events"},
		{"extract from is not a table", "SELECT EXTRACT(YEAR FROM ts) FROM events", "events"},
		{"from in string literal", "SELECT 'from admin' AS x FROM events", "events"},
		{"from in line comment", "SELECT 1 -- FROM events\nFROM t", "t"},
		{"SQL comment marker in block comment does not hide from", "SELECT 1 /* -- note */ FROM events", "events"},
		{"apostrophe in block comment does not hide from", "SELECT 1 /* don't stop */ FROM events", "events"},
		{"unclosed block still finds earlier from", "SELECT * FROM events /* keep", "events"},
		{"set prefix before select", "SET useMultistageEngine = true;\nSELECT * FROM logs", "logs"},
		{"from inside block comment is not the table", "SELECT 1 /* FROM events */ FROM t", "t"},
		{"trim from is not the table", `SELECT TRIM(BOTH 'x' FROM col) FROM events`, "events"},
		{"join uses left table", `SELECT * FROM orders AS o JOIN customers AS c ON o.id = c.id`, "orders"},
		{"hyphenated quoted table", `SELECT * FROM "my-table"`, "my-table"},
		{"subquery with no inner from", "SELECT * FROM (SELECT 1)", ""},
		{"no from", "SELECT 1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pinot.ExtractTableName(tt.sql))
		})
	}
}

func TestResolveTableName(t *testing.T) {
	t.Run("override wins when sql has no from", func(t *testing.T) {
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
	t.Run("error when neither override nor from", func(t *testing.T) {
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
		{"appends LIMIT after leading comment before SET", "/* note */ SET useMultistageEngine = true;\nSELECT 1", 100, "/* note */ SET useMultistageEngine = true;\nSELECT 1 LIMIT 100", false},
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
		{"bail on trailing line comment after block comment", "SELECT 1 FROM t/*note*/-- comment", 100, "SELECT 1 FROM t/*note*/-- comment", false},
		{"bail when limit is followed by trailing block comment", "SELECT 1 FROM events LIMIT 5 /* note */", 100, "SELECT 1 FROM events LIMIT 5 /* note */", false},
		{"bail when block comment is between LIMIT and row count", "SELECT 1 FROM events LIMIT /* note */ 5", 100, "SELECT 1 FROM events LIMIT /* note */ 5", false},
		{
			"bail when block comment is inside LIMIT on real table shape",
			"SELECT *  FROM faro_pinot_measurements_v1 LIMIT /* note */ 5",
			100,
			"SELECT *  FROM faro_pinot_measurements_v1 LIMIT /* note */ 5",
			false,
		},
		{"appends limit after leading block comment", "/* note */ SELECT 1 FROM events", 100, "/* note */ SELECT 1 FROM events LIMIT 100", false},
		{"appends LIMIT when delete is only a literal", "SELECT * FROM t WHERE action = 'delete'", 100, "SELECT * FROM t WHERE action = 'delete' LIMIT 100", false},
		{"appends LIMIT when -- is only a literal", "SELECT '--' FROM t", 100, "SELECT '--' FROM t LIMIT 100", false},
		{"appends LIMIT when UNION is only a literal", "SELECT 'UNION' FROM t", 100, "SELECT 'UNION' FROM t LIMIT 100", false},
		{"appends LIMIT when OPTION is only a literal", "SELECT * FROM t WHERE note = 'OPTION(timeoutMs=1)'", 100, "SELECT * FROM t WHERE note = 'OPTION(timeoutMs=1)' LIMIT 100", false},
		{"appends LIMIT when LIMIT offset,count is only a literal", "SELECT * FROM t WHERE hint = 'LIMIT 10, 20'", 100, "SELECT * FROM t WHERE hint = 'LIMIT 10, 20' LIMIT 100", false},
		{"appends LIMIT when UNION is only a quoted identifier", `SELECT "UNION" FROM t`, 100, `SELECT "UNION" FROM t LIMIT 100`, false},
		{"appends LIMIT when UNION is only in a line comment", "SELECT 1 -- UNION\nFROM t", 100, "SELECT 1 -- UNION\nFROM t LIMIT 100", false},
		{"appends LIMIT when block open is only in a line comment", "SELECT 1 -- /* note\nFROM t", 100, "SELECT 1 -- /* note\nFROM t LIMIT 100", false},
		{"appends LIMIT when UNION is only in a block comment", "SELECT 1 FROM t /* UNION */", 100, "SELECT 1 FROM t /* UNION */ LIMIT 100", false},
		{"bail on unclosed block comment", "SELECT 1 FROM t /* keep", 100, "SELECT 1 FROM t /* keep", false},
		{"appends LIMIT when apostrophe is only in a line comment", "SELECT a -- don't stop\nFROM events", 100, "SELECT a -- don't stop\nFROM events LIMIT 100", false},
		{"appends LIMIT when apostrophe is only in a block comment", "SELECT a /* don't stop */ FROM events", 100, "SELECT a /* don't stop */ FROM events LIMIT 100", false},
		{"appends LIMIT when a SQL comment marker is only inside a block comment", "SELECT /* note -- x */ a FROM events", 100, "SELECT /* note -- x */ a FROM events LIMIT 100", false},
		{"appends LIMIT when block comment holding a SQL comment marker is mid-statement", "SELECT a FROM events /* a -- b */ WHERE x = 1", 100, "SELECT a FROM events /* a -- b */ WHERE x = 1 LIMIT 100", false},
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

func TestLimitEnforcementNotice(t *testing.T) {
	flag := func(n int) *int { return &n }

	tests := []struct {
		name string
		expr string
		// limitFlag is --limit N. nil means the flag was omitted; enforcement
		// then uses pinot.DefaultLimit. This is not the SQL's own LIMIT.
		limitFlag *int
		want      string
	}{
		{
			name:      "limit 0 stays silent",
			expr:      "SELECT 1",
			limitFlag: flag(0),
			want:      "",
		},
		{
			name:      "omitted --limit appends default",
			expr:      "SELECT 1",
			limitFlag: nil,
			want:      "Query adjusted: appended default LIMIT 100. Use --limit 0 to disable enforcement.",
		},
		{
			name:      "set --limit appends requested",
			expr:      "SELECT 1",
			limitFlag: flag(50),
			want:      "Query adjusted: appended LIMIT 50 (--limit). Use --limit 0 to disable enforcement.",
		},
		{
			name:      "set --limit above max on append is reduced",
			expr:      "SELECT 1",
			limitFlag: flag(5000),
			want:      "Query adjusted: LIMIT reduced to 1000 (maximum). Use --limit 0 to disable enforcement.",
		},
		{
			name:      "omitted --limit caps bare LIMIT above max",
			expr:      "SELECT 1 LIMIT 5000",
			limitFlag: nil,
			want:      "Query adjusted: LIMIT reduced to 1000 (maximum). Use --limit 0 to disable enforcement.",
		},
		{
			name:      "set --limit caps bare LIMIT above max",
			expr:      "SELECT 1 LIMIT 5000",
			limitFlag: flag(50),
			want:      "You requested --limit 50 but the existing LIMIT was above the maximum, so it was reduced to 1000.",
		},
		{
			name:      "omitted --limit plus bare LIMIT under max stays silent",
			expr:      "SELECT 1 LIMIT 50",
			limitFlag: nil,
			want:      "",
		},
		{
			name:      "set --limit plus bare LIMIT under max is not applied",
			expr:      "SELECT 1 LIMIT 150",
			limitFlag: flag(50),
			want:      "You requested --limit 50 but the query already has a LIMIT, so no change was applied.",
		},
		{
			name:      "set --limit plus matching LIMIT stays silent",
			expr:      "SELECT 1 LIMIT 50",
			limitFlag: flag(50),
			want:      "",
		},
		{
			name:      "set --limit plus matching commented LIMIT stays silent",
			expr:      "SELECT 1 FROM events LIMIT /* note */ 50",
			limitFlag: flag(50),
			want:      "",
		},
		{
			name:      "set --limit plus commented LIMIT under max is not applied",
			expr:      "SELECT 1 FROM events LIMIT /* note */ 5",
			limitFlag: flag(50),
			want:      "You requested --limit 50 but the query already has a LIMIT, so no change was applied.",
		},
		{
			name:      "omitted --limit plus commented LIMIT under max stays silent",
			expr:      "SELECT *  FROM faro_pinot_measurements_v1 LIMIT /* note */ 5",
			limitFlag: nil,
			want:      "",
		},
		{
			name:      "set --limit plus commented LIMIT above max is unsafe",
			expr:      "SELECT 1 FROM events LIMIT /* note */ 5000",
			limitFlag: flag(50),
			want:      "The query has a LIMIT above 1000 that should be reduced to 1000, but the query is not safe to modify.",
		},
		{
			name:      "omitted --limit plus commented LIMIT above max is unsafe",
			expr:      "SELECT 1 FROM events LIMIT /* note */ 5000",
			limitFlag: nil,
			want:      "The query has a LIMIT above 1000 that should be reduced to 1000, but the query is not safe to modify.",
		},
		{
			name:      "union with LIMIT above max is unsafe when --limit set",
			expr:      "SELECT 1 FROM a UNION SELECT 2 FROM b LIMIT 2000",
			limitFlag: flag(50),
			want:      "The query has a LIMIT above 1000 that should be reduced to 1000, but the query is not safe to modify.",
		},
		{
			name:      "union with LIMIT above max is unsafe when --limit omitted",
			expr:      "SELECT 1 FROM a UNION SELECT 2 FROM b LIMIT 2000",
			limitFlag: nil,
			want:      "The query has a LIMIT above 1000 that should be reduced to 1000, but the query is not safe to modify.",
		},
		{
			name:      "union without LIMIT is not appended when --limit set",
			expr:      "SELECT 1 FROM a UNION SELECT 2 FROM b",
			limitFlag: flag(50),
			want:      "You asked for --limit 50, but a row limit was not appended (this query shape is not safe to modify). Add LIMIT in the SQL or use --limit 0.",
		},
		{
			name:      "union without LIMIT stays silent when --limit omitted",
			expr:      "SELECT 1 FROM a UNION SELECT 2 FROM b",
			limitFlag: nil,
			want:      "",
		},
		{
			name:      "OPTION is not appended when --limit set",
			expr:      "SELECT * FROM t OPTION(timeoutMs=5000)",
			limitFlag: flag(50),
			want:      "You asked for --limit 50, but a row limit was not appended (this query shape is not safe to modify). Add LIMIT in the SQL or use --limit 0.",
		},
		{
			name:      "trailing comment is not appended when --limit set",
			expr:      "SELECT 1 -- keep going",
			limitFlag: flag(50),
			want:      "You asked for --limit 50, but a row limit was not appended (this query shape is not safe to modify). Add LIMIT in the SQL or use --limit 0.",
		},
		{
			name:      "LIMIT offset,count already has a LIMIT when --limit set",
			expr:      "SELECT * FROM t LIMIT 10, 20",
			limitFlag: flag(50),
			want:      "You requested --limit 50 but the query already has a LIMIT, so no change was applied.",
		},
		{
			name:      "LIMIT offset,count above max stays silent when --limit omitted",
			expr:      "SELECT * FROM t LIMIT 200, 34678",
			limitFlag: nil,
			want:      "",
		},
		{
			name:      "LIMIT abc already has a LIMIT when --limit set",
			expr:      "SELECT * FROM t LIMIT abc",
			limitFlag: flag(50),
			want:      "You requested --limit 50 but the query already has a LIMIT, so no change was applied.",
		},
		{
			name:      "LIMIT OFFSET already has a LIMIT when --limit set",
			expr:      "SELECT * FROM t LIMIT 5000 OFFSET 0",
			limitFlag: flag(50),
			want:      "You requested --limit 50 but the query already has a LIMIT, so no change was applied.",
		},
		{
			name:      "LIMIT OFFSET stays silent when --limit omitted",
			expr:      "SELECT * FROM t LIMIT 5000 OFFSET 0",
			limitFlag: nil,
			want:      "",
		},
		{
			name:      "limit 0 on union stays silent",
			expr:      "SELECT 1 FROM a UNION SELECT 2 FROM b",
			limitFlag: flag(0),
			want:      "",
		},
		{
			name:      "EXPLAIN not SELECT-shaped stays silent",
			expr:      "EXPLAIN SELECT * FROM t",
			limitFlag: nil,
			want:      "",
		},
		{
			name:      "apostrophe in line comment appends default",
			expr:      "SELECT a -- don't stop\nFROM events",
			limitFlag: nil,
			want:      "Query adjusted: appended default LIMIT 100. Use --limit 0 to disable enforcement.",
		},
		{
			name:      "apostrophe in block comment appends default",
			expr:      "SELECT a /* don't stop */ FROM events",
			limitFlag: nil,
			want:      "Query adjusted: appended default LIMIT 100. Use --limit 0 to disable enforcement.",
		},
		{
			name:      "unclosed block comment is not appended when --limit set",
			expr:      "SELECT 1 FROM t /* keep",
			limitFlag: flag(50),
			want:      "You asked for --limit 50, but a row limit was not appended (this query shape is not safe to modify). Add LIMIT in the SQL or use --limit 0.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limitSet := tt.limitFlag != nil
			limit := pinot.DefaultLimit
			if limitSet {
				limit = *tt.limitFlag
			}
			sql, capped := pinot.EnforceLimit(tt.expr, limit, pinot.MaxLimit)
			got := pinot.LimitEnforcementNotice(tt.expr, sql, capped, limit, pinot.MaxLimit, limitSet)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLimitFlagUsage(t *testing.T) {
	assert.Contains(t, pinot.LimitFlagUsage(pinot.MaxLimit), "0 disables enforcement")
	assert.NotContains(t, pinot.LimitFlagUsage(pinot.MaxLimit), "UNION")
}
