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
