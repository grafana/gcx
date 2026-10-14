package postgres_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/postgres"
	"github.com/stretchr/testify/assert"
)

func TestBuildDescribeConstraintsQuery(t *testing.T) {
	query := postgres.BuildDescribeConstraintsQuery("public", "orders")
	assert.Contains(t, query, "FROM pg_constraint")
	assert.Contains(t, query, "unnest(c.conkey) WITH ORDINALITY")
	assert.Contains(t, query, "unnest(c.confkey) WITH ORDINALITY")
	assert.Contains(t, query, "src_ns.nspname = 'public'")
	assert.Contains(t, query, "src_cls.relname = 'orders'")
	assert.Contains(t, query, "ORDER BY c.conname, cols.ordinality")
}

func TestBuildDescribeConstraintsQueryEscapesLiterals(t *testing.T) {
	query := postgres.BuildDescribeConstraintsQuery("public'archive", "order'items")
	assert.Contains(t, query, "src_ns.nspname = 'public''archive'")
	assert.Contains(t, query, "src_cls.relname = 'order''items'")
}
