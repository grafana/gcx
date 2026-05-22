package clickhouse_test

import (
	"bytes"
	"testing"

	"github.com/grafana/gcx/internal/query/clickhouse"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTable(t *testing.T) {
	t.Run("renders columns and rows", func(t *testing.T) {
		resp := &clickhouse.QueryResponse{
			Columns: []clickhouse.Column{{Name: "id", Type: "number"}, {Name: "name", Type: "string"}},
			Rows:    [][]any{{float64(1), "alice"}, {float64(2), "bob"}},
		}
		var buf bytes.Buffer
		err := clickhouse.FormatTable(&buf, resp)
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "ID")
		assert.Contains(t, out, "NAME")
		assert.Contains(t, out, "alice")
		assert.Contains(t, out, "bob")
	})

	t.Run("formats time columns as RFC3339", func(t *testing.T) {
		resp := &clickhouse.QueryResponse{
			Columns: []clickhouse.Column{{Name: "timestamp", Type: "time"}, {Name: "service", Type: "string"}},
			Rows:    [][]any{{float64(1778601661000), "frontend"}, {float64(1778601721000), "backend"}},
		}
		var buf bytes.Buffer
		err := clickhouse.FormatTable(&buf, resp)
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "2026-05-12T")
		assert.NotContains(t, out, "1778601661000")
	})

	t.Run("empty result", func(t *testing.T) {
		resp := &clickhouse.QueryResponse{
			Columns: []clickhouse.Column{{Name: "x", Type: "string"}},
			Rows:    nil,
		}
		var buf bytes.Buffer
		err := clickhouse.FormatTable(&buf, resp)
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "No data")
	})
}

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
