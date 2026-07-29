package bigquery

import (
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/style"
)

// FormatStringList formats a single-column discovery result as a table.
func FormatStringList(w io.Writer, items []string, header string) error {
	if len(items) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable(header)
	for _, item := range items {
		t.Row(item)
	}
	return t.Render(w)
}

// FormatListTablesTable formats a slice of TableInfo as a human-readable table.
func FormatListTablesTable(w io.Writer, tables []TableInfo) error {
	if len(tables) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("NAME", "TYPE")
	for _, tbl := range tables {
		t.Row(tbl.Name, tbl.Type)
	}
	return t.Render(w)
}

// FormatDescribeTableTable formats a slice of ColumnInfo as a human-readable table.
func FormatDescribeTableTable(w io.Writer, cols []ColumnInfo) error {
	if len(cols) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("NAME", "TYPE", "NULLABLE")
	for _, c := range cols {
		t.Row(c.Name, c.Type, c.Nullable)
	}
	return t.Render(w)
}
