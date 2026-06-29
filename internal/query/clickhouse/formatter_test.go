package clickhouse_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatListTablesTable(t *testing.T) {
	totalRows := uint64(1000)
	totalBytes := uint64(4096)
	tables := []clickhouse.TableInfo{
		{Database: "default", Name: "events", Engine: "MergeTree", TotalRows: &totalRows, TotalBytes: &totalBytes},
		{Database: "default", Name: "mv_events", Engine: "MaterializedView", TotalRows: nil, TotalBytes: nil},
	}
	var buf bytes.Buffer
	err := clickhouse.FormatListTablesTable(&buf, tables)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "events")
	assert.Contains(t, out, "MergeTree")
	assert.Contains(t, out, "1000")
	assert.Contains(t, out, "-")
}

func TestFormatDescribeTableTable(t *testing.T) {
	cols := []clickhouse.ColumnInfo{
		{Name: "id", Type: "UInt64", DefaultType: "", DefaultExpression: "", Comment: "primary key"},
		{Name: "ts", Type: "DateTime64(9)", DefaultType: "DEFAULT", DefaultExpression: "now()", Comment: ""},
	}
	var buf bytes.Buffer
	err := clickhouse.FormatDescribeTableTable(&buf, cols)
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "id")
	assert.Contains(t, out, "UInt64")
	assert.Contains(t, out, "primary key")
	assert.Contains(t, out, "DEFAULT")
}
