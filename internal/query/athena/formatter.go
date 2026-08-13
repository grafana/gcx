package athena

import (
	"fmt"
	"io"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/style"
)

// FormatStringList formats a []string as a single-column table with the given header.
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

// FormatStringListArrow formats a []string as a single-column Arrow IPC
// payload with the given header.
func FormatStringListArrow(w io.Writer, items []string, header string) error {
	if len(items) == 0 {
		return nil
	}
	b := arrowtable.NewBuilder([]arrowtable.Field{arrowtable.Utf8(header)})
	for _, item := range items {
		b.Row(item)
	}
	return b.Write(w)
}
