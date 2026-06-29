package athena

import (
	"fmt"
	"io"

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
