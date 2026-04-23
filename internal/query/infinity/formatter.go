package infinity

import (
	"fmt"
	"io"
	"strings"

	"github.com/grafana/gcx/internal/style"
)

// FormatTable renders a QueryResponse as a terminal table.
func FormatTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Rows) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	headers := make([]string, len(resp.Columns))
	for i, col := range resp.Columns {
		headers[i] = strings.ToUpper(col.Name)
	}

	t := style.NewTable(headers...)
	for _, row := range resp.Rows {
		cells := make([]string, len(row))
		for i, val := range row {
			cells[i] = ToString(val)
		}
		t.Row(cells...)
	}

	return t.Render(w)
}
