package clickhouse

import (
	"fmt"
	"io"
	"strconv"

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

func formatNullableUint64(v *uint64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatUint(*v, 10)
}

// FormatListTablesCSV formats a slice of TableInfo as CSV. TOTAL_ROWS/
// TOTAL_BYTES are empty (not "-") for an absent value, so DuckDB infers a
// numeric column instead of falling back to VARCHAR.
func FormatListTablesCSV(w io.Writer, tables []TableInfo) error {
	if len(tables) == 0 {
		return nil
	}
	t := style.NewTable("DATABASE", "NAME", "ENGINE", "TOTAL_ROWS", "TOTAL_BYTES")
	for _, tbl := range tables {
		t.Row(tbl.Database, tbl.Name, tbl.Engine, formatNullableUint64CSV(tbl.TotalRows), formatNullableUint64CSV(tbl.TotalBytes))
	}
	return t.RenderCSV(w)
}

// FormatDescribeTableCSV formats a slice of ColumnInfo as CSV.
func FormatDescribeTableCSV(w io.Writer, cols []ColumnInfo) error {
	if len(cols) == 0 {
		return nil
	}
	t := style.NewTable("NAME", "TYPE", "DEFAULT_TYPE", "DEFAULT_EXPRESSION", "COMMENT")
	for _, c := range cols {
		t.Row(c.Name, c.Type, c.DefaultType, c.DefaultExpression, c.Comment)
	}
	return t.RenderCSV(w)
}

func formatNullableUint64CSV(v *uint64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatUint(*v, 10)
}
