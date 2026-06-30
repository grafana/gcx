package athena_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/athena"
	"github.com/stretchr/testify/assert"
)

func TestEnforceLimit(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		limit    int
		maxLimit int
		want     string
	}{
		{name: "appends default limit", sql: "SELECT * FROM events", limit: 100, maxLimit: 1000, want: "SELECT * FROM events LIMIT 100"},
		{name: "caps existing limit", sql: "SELECT * FROM events LIMIT 5000", limit: 100, maxLimit: 1000, want: "SELECT * FROM events LIMIT 1000"},
		{name: "preserves small limit", sql: "SELECT * FROM events LIMIT 50", limit: 100, maxLimit: 1000, want: "SELECT * FROM events LIMIT 50"},
		{name: "skips SHOW", sql: "SHOW DATABASES", limit: 100, maxLimit: 1000, want: "SHOW DATABASES"},
		{name: "skips DESCRIBE", sql: "DESCRIBE my_table", limit: 100, maxLimit: 1000, want: "DESCRIBE my_table"},
		{name: "skips show lowercase", sql: "show tables", limit: 100, maxLimit: 1000, want: "show tables"},
		{name: "bails on LIMIT OFFSET", sql: "SELECT * FROM events LIMIT 100 OFFSET 10", limit: 100, maxLimit: 1000, want: "SELECT * FROM events LIMIT 100 OFFSET 10"},
		{name: "bails on LIMIT ALL OFFSET", sql: "SELECT * FROM events LIMIT ALL OFFSET 5", limit: 100, maxLimit: 1000, want: "SELECT * FROM events LIMIT ALL OFFSET 5"},
		{name: "case insensitive limit detection", sql: "SELECT * FROM events limit 50", limit: 100, maxLimit: 1000, want: "SELECT * FROM events limit 50"},
		{name: "bails on EXPLAIN", sql: "EXPLAIN SELECT * FROM events", limit: 100, maxLimit: 1000, want: "EXPLAIN SELECT * FROM events"},
		{name: "bails on EXPLAIN ANALYZE", sql: "EXPLAIN ANALYZE SELECT * FROM events", limit: 100, maxLimit: 1000, want: "EXPLAIN ANALYZE SELECT * FROM events"},
		{name: "bails on lowercase explain", sql: "explain select * from events", limit: 100, maxLimit: 1000, want: "explain select * from events"},
		{name: "disabled when zero", sql: "SELECT * FROM events", limit: 0, maxLimit: 1000, want: "SELECT * FROM events"},
		{name: "strips trailing semicolon", sql: "SELECT * FROM events;", limit: 100, maxLimit: 1000, want: "SELECT * FROM events LIMIT 100;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := athena.EnforceLimit(tt.sql, tt.limit, tt.maxLimit)
			assert.Equal(t, tt.want, got)
		})
	}
}
