package infinity

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/arrowtable"
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

// FormatArrow renders a QueryResponse as an Arrow IPC payload, typing each
// column from its declared Column.Type: "time" -> Timestamp, "number" ->
// Float64, "boolean" -> Boolean, anything else -> Utf8 (via ToString, same
// as FormatTable's cell rendering).
func FormatArrow(w io.Writer, resp *QueryResponse) error {
	if len(resp.Rows) == 0 {
		return nil
	}

	fields := make([]arrowtable.Field, len(resp.Columns))
	for i, col := range resp.Columns {
		fields[i] = arrowFieldForColumn(col)
	}
	b := arrowtable.NewBuilder(fields)

	for _, row := range resp.Rows {
		vals := make([]any, len(fields))
		// copy tolerates a row with fewer or more cells than resp.Columns —
		// data from an arbitrary external endpoint isn't guaranteed to be
		// perfectly rectangular; missing cells become null, extras are
		// dropped, rather than panicking on an upstream data quirk.
		copy(vals, row)
		for i, v := range vals {
			colType := ""
			if i < len(resp.Columns) {
				colType = resp.Columns[i].Type
			}
			vals[i] = arrowColumnValue(v, colType)
		}
		b.Row(vals...)
	}

	return b.Write(w)
}

func arrowFieldForColumn(col Column) arrowtable.Field {
	name := strings.ToUpper(col.Name)
	switch col.Type {
	case "time":
		return arrowtable.Timestamp(name)
	case "number":
		return arrowtable.Float64(name)
	case "boolean":
		return arrowtable.Boolean(name)
	default:
		return arrowtable.Utf8(name)
	}
}

// arrowColumnValue converts a raw cell value to the Go type its Arrow column
// expects, returning nil (null) when v is absent or doesn't match colType.
func arrowColumnValue(v any, colType string) any {
	if v == nil {
		return nil
	}
	switch colType {
	case "time":
		switch t := v.(type) {
		case float64:
			return time.UnixMilli(int64(t)).UTC()
		case string:
			ms, err := strconv.ParseFloat(t, 64)
			if err != nil {
				return nil
			}
			return time.UnixMilli(int64(ms)).UTC()
		default:
			return nil
		}
	case "number":
		switch n := v.(type) {
		case float64:
			return n
		case int64:
			return float64(n)
		case string:
			f, err := strconv.ParseFloat(n, 64)
			if err != nil {
				return nil
			}
			return f
		default:
			return nil
		}
	case "boolean":
		if b, ok := v.(bool); ok {
			return b
		}
		return nil
	default:
		return ToString(v)
	}
}
