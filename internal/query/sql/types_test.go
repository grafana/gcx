package sql_test

import (
	"strings"
	"testing"

	"github.com/grafana/gcx/internal/query/sql"
	"github.com/stretchr/testify/assert"
)

func TestEnforceLimit(t *testing.T) {
	// bail mimics a dialect that opts out of EXPLAIN statements.
	bail := func(s string) bool {
		return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s)), "EXPLAIN")
	}

	tests := []struct {
		name     string
		sql      string
		limit    int
		maxLimit int
		bail     func(string) bool
		want     string
	}{
		{name: "appends default limit", sql: "SELECT 1", limit: 100, maxLimit: 1000, want: "SELECT 1 LIMIT 100"},
		{name: "caps existing limit", sql: "SELECT 1 LIMIT 5000", limit: 100, maxLimit: 1000, want: "SELECT 1 LIMIT 1000"},
		{name: "preserves small limit", sql: "SELECT 1 LIMIT 50", limit: 100, maxLimit: 1000, want: "SELECT 1 LIMIT 50"},
		{name: "strips trailing semicolon", sql: "SELECT 1;", limit: 100, maxLimit: 1000, want: "SELECT 1 LIMIT 100;"},
		{name: "disabled when zero", sql: "SELECT 1", limit: 0, maxLimit: 1000, want: "SELECT 1"},
		{name: "nil bail is allowed", sql: "SELECT 1", limit: 100, maxLimit: 1000, bail: func(string) bool { return false }, want: "SELECT 1 LIMIT 100"},
		{name: "bail passes through", sql: "EXPLAIN SELECT 1", limit: 100, maxLimit: 1000, bail: bail, want: "EXPLAIN SELECT 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sql.EnforceLimit(tt.sql, tt.limit, tt.maxLimit, tt.bail)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEffectiveLimit(t *testing.T) {
	assert.Equal(t, 100, sql.EffectiveLimit(100, 1000))
	assert.Equal(t, 1000, sql.EffectiveLimit(1000, 1000))
	assert.Equal(t, 1000, sql.EffectiveLimit(5000, 1000), "clamps above max")
	assert.Equal(t, 0, sql.EffectiveLimit(0, 1000), "0 disables")
	assert.Equal(t, 0, sql.EffectiveLimit(-5, 1000), "negative disables")
}

func TestEnforceLimitSentinel(t *testing.T) {
	tests := []struct {
		name       string
		sql        string
		limit      int
		maxLimit   int
		wantSQL    string
		wantEff    int
		wantCapped bool
	}{
		{name: "injects eff+1 sentinel", sql: "SELECT 1", limit: 100, maxLimit: 1000, wantSQL: "SELECT 1 LIMIT 101", wantEff: 100, wantCapped: true},
		{name: "sentinel at the ceiling detects >max", sql: "SELECT 1", limit: 1000, maxLimit: 1000, wantSQL: "SELECT 1 LIMIT 1001", wantEff: 1000, wantCapped: true},
		{name: "above ceiling clamps eff, still sentinels", sql: "SELECT 1", limit: 5000, maxLimit: 1000, wantSQL: "SELECT 1 LIMIT 1001", wantEff: 1000, wantCapped: true},
		{name: "disabled when zero", sql: "SELECT 1", limit: 0, maxLimit: 1000, wantSQL: "SELECT 1", wantEff: 0, wantCapped: false},
		{name: "user LIMIT within max is respected, not capped", sql: "SELECT 1 LIMIT 50", limit: 100, maxLimit: 1000, wantSQL: "SELECT 1 LIMIT 50", wantEff: 100, wantCapped: false},
		{name: "oversized user LIMIT clamped to real max, not sentinel", sql: "SELECT 1 LIMIT 5000", limit: 100, maxLimit: 1000, wantSQL: "SELECT 1 LIMIT 1000", wantEff: 100, wantCapped: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, gotEff, gotCapped := sql.EnforceLimitSentinel(tt.sql, tt.limit, tt.maxLimit, nil)
			assert.Equal(t, tt.wantSQL, gotSQL)
			assert.Equal(t, tt.wantEff, gotEff)
			assert.Equal(t, tt.wantCapped, gotCapped)
		})
	}
}

func TestQueryResponseTruncate(t *testing.T) {
	rows := func(n int) [][]any {
		out := make([][]any, n)
		for i := range out {
			out[i] = []any{i}
		}
		return out
	}

	tests := []struct {
		name     string
		rows     int
		eff      int
		wantRows int
		wantDrop bool
	}{
		{name: "drops surplus and reports truncation", rows: 101, eff: 100, wantRows: 100, wantDrop: true},
		{name: "exact fit is not truncated", rows: 100, eff: 100, wantRows: 100, wantDrop: false},
		{name: "fewer rows than cap", rows: 42, eff: 100, wantRows: 42, wantDrop: false},
		{name: "eff 0 is a no-op", rows: 500, eff: 0, wantRows: 500, wantDrop: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &sql.QueryResponse{Rows: rows(tt.rows)}
			got := resp.Truncate(tt.eff)
			assert.Equal(t, tt.wantDrop, got)
			assert.Len(t, resp.Rows, tt.wantRows)
		})
	}
}

func TestTruncationHint(t *testing.T) {
	hint := sql.TruncationHint(100, 1000)
	assert.Contains(t, hint, "first 100 rows")
	assert.Contains(t, hint, "max 1000")
	assert.Contains(t, hint, "--limit 0")
}
