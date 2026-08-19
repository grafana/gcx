package sql

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/style"
)

// FormatTable formats a QueryResponse as a human-readable table.
func FormatTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Rows) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

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
				vals[i] = formatValue(v)
			}
		}
		t.Row(vals...)
	}
	return t.Render(w)
}

// FormatWideTable formats a QueryResponse as a wide table. SQL datasource
// results are inherently flat, so this delegates to FormatTable.
func FormatWideTable(w io.Writer, resp *QueryResponse) error {
	return FormatTable(w, resp)
}

// FormatArrow formats a QueryResponse as an Arrow IPC payload. A column
// declared Type "time" becomes a Timestamp column; otherwise it becomes
// Float64 when every non-null cell it holds is a float64, else Utf8
// (rendered the same way formatValue does for display) — shared by both
// ClickHouse and Athena, which query via this common response shape.
func FormatArrow(w io.Writer, resp *QueryResponse) error {
	if len(resp.Rows) == 0 {
		return nil
	}

	numeric := make([]bool, len(resp.Columns))
	for i, col := range resp.Columns {
		if col.Type != "time" {
			numeric[i] = columnIsAllFloat64(resp.Rows, i)
		}
	}

	fields := make([]arrowtable.Field, len(resp.Columns))
	for i, col := range resp.Columns {
		switch {
		case col.Type == "time":
			fields[i] = arrowtable.Timestamp(strings.ToUpper(col.Name))
		case numeric[i]:
			fields[i] = arrowtable.Float64(strings.ToUpper(col.Name))
		default:
			fields[i] = arrowtable.Utf8(strings.ToUpper(col.Name))
		}
	}
	b := arrowtable.NewBuilder(fields)

	for _, row := range resp.Rows {
		vals := make([]any, len(fields))
		copy(vals, row)
		for i, v := range vals {
			switch {
			case v == nil:
				// leave as nil
			case resp.Columns[i].Type == "time":
				vals[i] = arrowTimestamp(v)
			case numeric[i]:
				vals[i] = arrowFloat(v)
			default:
				vals[i] = formatValue(v)
			}
		}
		b.Row(vals...)
	}

	return b.Write(w)
}

// columnIsAllFloat64 reports whether every non-null cell in rows[*][col] is a
// float64, with at least one such cell present.
func columnIsAllFloat64(rows [][]any, col int) bool {
	seen := false
	for _, row := range rows {
		if col >= len(row) || row[col] == nil {
			continue
		}
		if _, ok := row[col].(float64); !ok {
			return false
		}
		seen = true
	}
	return seen
}

// arrowTimestamp converts a millisecond-epoch value (float64 or numeric
// string, same wire shapes formatTimestamp accepts) to a time.Time, or nil
// if it doesn't parse.
func arrowTimestamp(v any) any {
	switch ts := v.(type) {
	case float64:
		return time.UnixMilli(int64(ts)).UTC()
	case string:
		ms, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return nil
		}
		return time.UnixMilli(ms).UTC()
	default:
		return nil
	}
}

// arrowFloat returns v as a float64, or nil if it isn't one.
func arrowFloat(v any) any {
	if f, ok := v.(float64); ok {
		return f
	}
	return nil
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
	switch val := v.(type) {
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
