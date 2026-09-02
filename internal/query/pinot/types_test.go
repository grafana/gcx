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
				require.Error(t, err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
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
		{"simple from", `SELECT count(*) FROM faro_pinot_events_v2`, "faro_pinot_events_v2"},
		{"quoted from", `SELECT 1 FROM "faro_pinot_events_v2"`, "faro_pinot_events_v2"},
		{"set prefix", "SET useMultistageEngine = true;\nSELECT * FROM faro_pinot_logs_v1", "faro_pinot_logs_v1"},
		{"subquery from skipped", "SELECT * FROM (SELECT 1)", ""},
		{"no from", "SELECT 1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pinot.ExtractTableName(tt.sql))
		})
	}
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
		{"bail on EXPLAIN", "EXPLAIN SELECT * FROM t", 100, "EXPLAIN SELECT * FROM t", false},
		{"bail on DML INSERT", "INSERT INTO t VALUES (1)", 100, "INSERT INTO t VALUES (1)", false},
		{"bail on trailing line comment", "SELECT 1 -- keep going", 100, "SELECT 1 -- keep going", false},
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
