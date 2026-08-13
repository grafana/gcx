package clickhouse_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/arrowtable"
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

func TestFormatListTablesArrow(t *testing.T) {
	totalRows := uint64(1000)
	tables := []clickhouse.TableInfo{
		{Database: "default", Name: "events", Engine: "MergeTree", TotalRows: &totalRows, TotalBytes: nil},
	}

	var buf bytes.Buffer
	require.NoError(t, clickhouse.FormatListTablesArrow(&buf, tables))

	headers, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"DATABASE", "NAME", "ENGINE", "TOTAL_ROWS", "TOTAL_BYTES"}, headers)
	require.Len(t, rows, 1)
	assert.Equal(t, "events", rows[0][1])
	assert.EqualValues(t, 1000, rows[0][3])
	assert.Nil(t, rows[0][4])
}

func TestFormatListTablesArrow_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, clickhouse.FormatListTablesArrow(&buf, nil))
	assert.Empty(t, buf.String())
}

func TestFormatDescribeTableArrow(t *testing.T) {
	cols := []clickhouse.ColumnInfo{
		{Name: "id", Type: "UInt64", Comment: "primary key"},
	}

	var buf bytes.Buffer
	require.NoError(t, clickhouse.FormatDescribeTableArrow(&buf, cols))

	headers, rows, err := arrowtable.ReadStream(&buf)
	require.NoError(t, err)
	assert.Equal(t, []string{"NAME", "TYPE", "DEFAULT_TYPE", "DEFAULT_EXPRESSION", "COMMENT"}, headers)
	require.Len(t, rows, 1)
	assert.Equal(t, "id", rows[0][0])
	assert.Equal(t, "UInt64", rows[0][1])
	assert.Equal(t, "primary key", rows[0][4])
}

func TestFormatDescribeTableArrow_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, clickhouse.FormatDescribeTableArrow(&buf, nil))
	assert.Empty(t, buf.String())
}
