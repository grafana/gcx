package sql

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// FormatTable formats a QueryResponse as a human-readable table.
func FormatTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Rows) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	return buildTable(resp, formatValue).Render(w)
}

// FormatWideTable formats a QueryResponse as a wide table. SQL datasource
// results are inherently flat, so this delegates to FormatTable.
func FormatWideTable(w io.Writer, resp *QueryResponse) error {
	return FormatTable(w, resp)
}

// FormatCSV formats a QueryResponse as CSV, same columns as FormatTable. A
// nil value renders as an empty field rather than table's "-", so a NULL
// stays distinguishable from the literal string "-" and doesn't force the
// whole column to VARCHAR in a CSV-consuming query engine.
func FormatCSV(w io.Writer, resp *QueryResponse) error {
	return buildTable(resp, formatCSVValue).RenderCSV(w)
}

func buildTable(resp *QueryResponse, formatVal func(any) string) *style.TableBuilder {
	timeColumns := make(map[int]bool, len(resp.Columns))
	headers := make([]string, len(resp.Columns))
	for i, col := range resp.Columns {
		headers[i] = strings.ToUpper(col.Name)
		if col.Type == "time" {
			timeColumns[i] = true
		}
	}

	t := style.NewTable(headers...)
	for _, row := range resp.Rows {
		vals := make([]string, len(row))
		for i, v := range row {
			if timeColumns[i] {
				vals[i] = formatTimestamp(v)
			} else {
				vals[i] = formatVal(v)
			}
		}
		t.Row(vals...)
	}
	return t
}

func formatTimestamp(v any) string {
	switch ts := v.(type) {
	case float64:
		return time.UnixMilli(int64(ts)).UTC().Format(time.RFC3339)
	case string:
		ms, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return ts
		}
		return time.UnixMilli(ms).UTC().Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func formatValue(v any) string {
	if v == nil {
		return "-"
	}
	return formatNonNilValue(v)
}

func formatCSVValue(v any) string {
	if v == nil {
		return ""
	}
	return formatNonNilValue(v)
}

func formatNonNilValue(val any) string {
	switch val := val.(type) {
	case float64:
		if val == float64(int64(val)) {
			return strconv.FormatInt(int64(val), 10)
		}
		return fmt.Sprintf("%g", val)
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}
