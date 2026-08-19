package clickhouse

import (
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/style"
)

// FormatListTablesTable formats a slice of TableInfo as a human-readable table.
func FormatListTablesTable(w io.Writer, tables []TableInfo) error {
	if len(tables) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("DATABASE", "NAME", "ENGINE", "TOTAL_ROWS", "TOTAL_BYTES")
	for _, tbl := range tables {
		t.Row(tbl.Database, tbl.Name, tbl.Engine, formatNullableUint64(tbl.TotalRows), formatNullableUint64(tbl.TotalBytes))
	}
	return t.Render(w)
}

// FormatDescribeTableTable formats a slice of ColumnInfo as a human-readable table.
func FormatDescribeTableTable(w io.Writer, cols []ColumnInfo) error {
	if len(cols) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("NAME", "TYPE", "DEFAULT_TYPE", "DEFAULT_EXPRESSION", "COMMENT")
	for _, c := range cols {
		t.Row(c.Name, c.Type, c.DefaultType, c.DefaultExpression, c.Comment)
	}
	return t.Render(w)
}

// FormatListTablesArrow formats a slice of TableInfo as an Arrow IPC payload.
// TOTAL_ROWS/TOTAL_BYTES are real nullable Int64 columns instead of "-" for
// absent values.
func FormatListTablesArrow(w io.Writer, tables []TableInfo) error {
	if len(tables) == 0 {
		return nil
	}
	b := arrowtable.NewBuilder([]arrowtable.Field{
		arrowtable.Utf8("DATABASE"),
		arrowtable.Utf8("NAME"),
		arrowtable.Utf8("ENGINE"),
		arrowtable.Int64("TOTAL_ROWS"),
		arrowtable.Int64("TOTAL_BYTES"),
	})
	for _, tbl := range tables {
		b.Row(tbl.Database, tbl.Name, tbl.Engine, nullableUint64ToArrow(tbl.TotalRows), nullableUint64ToArrow(tbl.TotalBytes))
	}
	return b.Write(w)
}

// FormatDescribeTableArrow formats a slice of ColumnInfo as an Arrow IPC payload.
func FormatDescribeTableArrow(w io.Writer, cols []ColumnInfo) error {
	if len(cols) == 0 {
		return nil
	}
	b := arrowtable.NewBuilder([]arrowtable.Field{
		arrowtable.Utf8("NAME"),
		arrowtable.Utf8("TYPE"),
		arrowtable.Utf8("DEFAULT_TYPE"),
		arrowtable.Utf8("DEFAULT_EXPRESSION"),
		arrowtable.Utf8("COMMENT"),
	})
	for _, c := range cols {
		b.Row(c.Name, c.Type, c.DefaultType, c.DefaultExpression, c.Comment)
	}
	return b.Write(w)
}

// nullableUint64ToArrow converts a nullable uint64 to an Int64-column value,
// returning nil for both a nil pointer and a value too large for int64
// (ClickHouse row/byte counts realistically never approach that ceiling).
func nullableUint64ToArrow(v *uint64) any {
	if v == nil || *v > math.MaxInt64 {
		return nil
	}
	return int64(*v)
}

func formatNullableUint64(v *uint64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatUint(*v, 10)
}
