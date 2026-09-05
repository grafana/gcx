package mysql_test

import (
	"testing"

	"github.com/grafana/gcx/internal/query/mysql"
	"github.com/stretchr/testify/assert"
)

func TestBuildDescribeConstraintsQuery(t *testing.T) {
	query := mysql.BuildDescribeConstraintsQuery("sales", "order_items")
	assert.Contains(t, query, "information_schema.TABLE_CONSTRAINTS")
	assert.Contains(t, query, "KEY_COLUMN_USAGE")
	assert.Contains(t, query, "REFERENTIAL_CONSTRAINTS")
	assert.Contains(t, query, "tc.TABLE_SCHEMA = 'sales'")
	assert.Contains(t, query, "tc.TABLE_NAME = 'order_items'")
	assert.Contains(t, query, "ORDER BY tc.CONSTRAINT_NAME, kcu.ORDINAL_POSITION")
}

func TestBuildDescribeConstraintsQueryEscapesLiterals(t *testing.T) {
	query := mysql.BuildDescribeConstraintsQuery("sales'archive", "order'items")
	assert.Contains(t, query, "tc.TABLE_SCHEMA = 'sales''archive'")
	assert.Contains(t, query, "tc.TABLE_NAME = 'order''items'")
}
