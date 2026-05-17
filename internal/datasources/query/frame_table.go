package query

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// FrameField is the minimal field shape needed to render a frame as a table.
type FrameField struct {
	Name string
	Type string
}

// FormatFrameTable renders a single Grafana data frame as a table. Values is
// column-major: values[col][row]. Empty rows produce an empty table with
// just the header row.
func FormatFrameTable(w io.Writer, fields []FrameField, values [][]any) error {
	headers := make([]string, len(fields))
	for i, f := range fields {
		headers[i] = f.Name
	}

	tb := style.NewTable(headers...)

	rowCount := 0
	for _, col := range values {
		if len(col) > rowCount {
			rowCount = len(col)
		}
	}

	for r := range rowCount {
		row := make([]string, len(fields))
		for c := range fields {
			var v any
			if c < len(values) && r < len(values[c]) {
				v = values[c][r]
			}
			row[c] = formatCell(v, typeOf(fields, c))
		}
		tb.Row(row...)
	}

	return tb.Render(w)
}

func typeOf(fields []FrameField, c int) string {
	if c < 0 || c >= len(fields) {
		return ""
	}
	return fields[c].Type
}

// formatCell renders a single cell value. For "time" fields it converts
// epoch milliseconds (numeric) into RFC3339; everything else is stringified
// with sensible defaults.
func formatCell(v any, fieldType string) string {
	if v == nil {
		return ""
	}
	if fieldType == "time" {
		if ms, ok := toInt64(v); ok {
			return time.UnixMilli(ms).UTC().Format(time.RFC3339)
		}
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case float32:
		return int64(x), true
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case string:
		n, err := strconv.ParseInt(x, 10, 64)
		if err == nil {
			return n, true
		}
		f, err := strconv.ParseFloat(x, 64)
		if err == nil {
			return int64(f), true
		}
	}
	return 0, false
}
