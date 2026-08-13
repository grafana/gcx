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
	return buildStringListTable(items, header).Render(w)
}

// FormatStringListCSV formats a []string as single-column CSV with the given header.
func FormatStringListCSV(w io.Writer, items []string, header string) error {
	if len(items) == 0 {
		return nil
	}
	return buildStringListTable(items, header).RenderCSV(w)
}

func buildStringListTable(items []string, header string) *style.TableBuilder {
	t := style.NewTable(header)
	for _, item := range items {
		t.Row(item)
	}
	return t
}
