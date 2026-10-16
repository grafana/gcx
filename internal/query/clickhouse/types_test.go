package clickhouse_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/stretchr/testify/assert"
)

func TestEscapeSQLString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"normal name", "my_table", "my_table"},
		{"dotted name", "db.table", "db.table"},
		{"single quote", "it's", "it''s"},
		{"doubled quotes passthrough", "it''s", "it''''s"},
		{"empty string", "", ""},
		{"unicode", "données", "données"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clickhouse.EscapeSQLString(tt.input))
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
		{"multiline DESC keeps limit", "SELECT * FROM events\nORDER BY ts\nDESC", 100, "SELECT * FROM events\nORDER BY ts\nDESC LIMIT 100"},
		{"indented lowercase desc keeps limit", "SELECT * FROM events\nORDER BY ts\n  desc;", 100, "SELECT * FROM events\nORDER BY ts\n  desc LIMIT 100;"},
		{"multiline EXISTS keeps limit", "SELECT * FROM events WHERE\nEXISTS (SELECT 1)", 100, "SELECT * FROM events WHERE\nEXISTS (SELECT 1) LIMIT 100"},
		{"multiline CHECK keeps limit", "SELECT\ncheck\nFROM events", 100, "SELECT\ncheck\nFROM events LIMIT 100"},
		{"multiline DESC caps existing LIMIT", "SELECT * FROM events ORDER BY ts\nDESC LIMIT 5000", 100, "SELECT * FROM events ORDER BY ts\nDESC LIMIT 1000"},
		{"multiline limit 0 disables enforcement", "SELECT * FROM events ORDER BY ts\nDESC", 0, "SELECT * FROM events ORDER BY ts\nDESC"},
		{"bail on leading whitespace describe", " \n\tdescribe table events", 100, " \n\tdescribe table events"},
		{"bail on leading whitespace explain", "\n  EXPLAIN SELECT * FROM events", 100, "\n  EXPLAIN SELECT * FROM events"},
		{"appends LIMIT with trailing semicolon", "SELECT 1;", 100, "SELECT 1 LIMIT 100;"},
		{"keeps existing LIMIT if under max", "SELECT 1 LIMIT 50", 100, "SELECT 1 LIMIT 50"},
		{"caps existing LIMIT exceeding max", "SELECT 1 LIMIT 5000", 1000, "SELECT 1 LIMIT 1000"},
		{"case insensitive LIMIT detection", "SELECT 1 limit 50", 100, "SELECT 1 limit 50"},
		{"bail on LIMIT BY", "SELECT col, count() FROM t GROUP BY col LIMIT 5 BY col", 100, "SELECT col, count() FROM t GROUP BY col LIMIT 5 BY col"},
		{"bail on FORMAT", "SELECT 1 FORMAT JSON", 100, "SELECT 1 FORMAT JSON"},
		{"bail on SETTINGS", "SELECT 1 SETTINGS max_threads=1", 100, "SELECT 1 SETTINGS max_threads=1"},
		{"bail on LIMIT OFFSET", "SELECT * FROM t LIMIT 100 OFFSET 10", 100, "SELECT * FROM t LIMIT 100 OFFSET 10"},
		{"bail on LIMIT comma syntax", "SELECT * FROM t LIMIT 10, 100", 100, "SELECT * FROM t LIMIT 10, 100"},
		{"bail on EXPLAIN", "EXPLAIN SELECT * FROM t", 100, "EXPLAIN SELECT * FROM t"},
		{"bail on EXPLAIN PIPELINE", "EXPLAIN PIPELINE SELECT * FROM t", 100, "EXPLAIN PIPELINE SELECT * FROM t"},
		{"bail on EXPLAIN with options", "EXPLAIN indexes=1 SELECT * FROM t", 100, "EXPLAIN indexes=1 SELECT * FROM t"},
		{"bail on lowercase explain", "explain select * from t", 100, "explain select * from t"},
		{"bail on DESCRIBE TABLE", "DESCRIBE TABLE otel_email.otel_logs", 100, "DESCRIBE TABLE otel_email.otel_logs"},
		{"bail on DESC TABLE", "DESC otel_email.otel_logs", 100, "DESC otel_email.otel_logs"},
		{"bail on SHOW CREATE TABLE", "SHOW CREATE TABLE otel_email.otel_logs", 100, "SHOW CREATE TABLE otel_email.otel_logs"},
		{"bail on EXISTS TABLE", "EXISTS TABLE otel_email.otel_logs", 100, "EXISTS TABLE otel_email.otel_logs"},
		{"bail on CHECK TABLE", "CHECK TABLE otel_email.otel_logs", 100, "CHECK TABLE otel_email.otel_logs"},
		{"bail on lowercase describe", "describe table otel_email.otel_logs", 100, "describe table otel_email.otel_logs"},
		{"limit 0 disables enforcement", "SELECT 1", 0, "SELECT 1"},
		{"caps LIMIT at max", "SELECT 1 LIMIT 2000", 1000, "SELECT 1 LIMIT 1000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clickhouse.EnforceLimit(tt.sql, tt.limit, 1000)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnforceLimitSentinel(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		want       string
		wantCapped bool
	}{
		{"multiline DESC", "SELECT * FROM events ORDER BY ts\nDESC", "SELECT * FROM events ORDER BY ts\nDESC LIMIT 101", true},
		{"multiline EXISTS", "SELECT * FROM events WHERE\nEXISTS (SELECT 1)", "SELECT * FROM events WHERE\nEXISTS (SELECT 1) LIMIT 101", true},
		{"multiline CHECK", "SELECT\ncheck\nFROM events", "SELECT\ncheck\nFROM events LIMIT 101", true},
		{"leading whitespace DESCRIBE", " \n\tdescribe table events", " \n\tdescribe table events", false},
		{"leading whitespace EXPLAIN", "\n  EXPLAIN SELECT 1", "\n  EXPLAIN SELECT 1", false},
		{"mid-statement SETTINGS", "SELECT 1\nSETTINGS max_threads=1", "SELECT 1\nSETTINGS max_threads=1", false},
		{"mid-statement FORMAT", "SELECT 1\nFORMAT JSON", "SELECT 1\nFORMAT JSON", false},
		{"mid-statement LIMIT BY", "SELECT * FROM events\nLIMIT 10 BY ts", "SELECT * FROM events\nLIMIT 10 BY ts", false},
		{"mid-statement LIMIT OFFSET", "SELECT * FROM events\nLIMIT 10 OFFSET 5", "SELECT * FROM events\nLIMIT 10 OFFSET 5", false},
		{"mid-statement LIMIT comma", "SELECT * FROM events\nLIMIT 5, 10", "SELECT * FROM events\nLIMIT 5, 10", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, eff, capped := clickhouse.EnforceLimitSentinel(tt.sql, 100, 1000)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, 100, eff)
			assert.Equal(t, tt.wantCapped, capped)
		})
	}
}

func TestValidateIdentifier(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid simple", "my_table", false},
		{"valid dotted", "db.table_name", false},
		{"valid underscore prefix", "_internal", false},
		{"empty is ok", "", false},
		{"has single quote", "it's", true},
		{"has semicolon", "t; DROP TABLE", true},
		{"has space", "my table", true},
		{"starts with number", "1table", true},
		{"has backtick", "t`ble", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := clickhouse.ValidateIdentifier(tt.input, "table")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
