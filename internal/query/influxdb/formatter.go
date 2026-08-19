package influxdb

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/grafana/gcx/internal/arrowtable"
	"github.com/grafana/gcx/internal/style"
)

// FormatQueryTable formats a QueryResponse as a table.
func FormatQueryTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Rows) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	t := style.NewTable(resp.Columns...)
	for _, row := range resp.Rows {
		vals := make([]string, len(row))
		for i, v := range row {
			if resp.TimeColumns[i] {
				vals[i] = formatTimestampMs(v)
			} else {
				vals[i] = formatValue(v)
			}
		}
		t.Row(vals...)
	}

	return t.Render(w)
}

// formatTimestampMs converts a millisecond-epoch value to RFC3339.
func formatTimestampMs(v any) string {
	var ms int64
	switch val := v.(type) {
	case float64:
		if val == 0 {
			return fmt.Sprintf("%v", v)
		}
		ms = int64(val)
	case int64:
		if val == 0 {
			return fmt.Sprintf("%v", v)
		}
		ms = val
	default:
		return fmt.Sprintf("%v", v)
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

// formatValue formats a non-time cell value, using decimal notation for floats.
func formatValue(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprintf("%v", v)
}

// FormatArrow formats a QueryResponse as an Arrow IPC payload. TimeColumns
// become Timestamp columns; a non-time column becomes Float64 when every
// non-null cell it holds is a float64, otherwise Utf8 (rendered the same way
// formatValue does for display) — InfluxDB doesn't declare a column's type
// up front, so it's inferred from the data actually returned.
func FormatArrow(w io.Writer, resp *QueryResponse) error {
	if len(resp.Rows) == 0 {
		return nil
	}

	numeric := make([]bool, len(resp.Columns))
	for i := range resp.Columns {
		if !resp.TimeColumns[i] {
			numeric[i] = columnIsAllFloat64(resp.Rows, i)
		}
	}

	fields := make([]arrowtable.Field, len(resp.Columns))
	for i, name := range resp.Columns {
		switch {
		case resp.TimeColumns[i]:
			fields[i] = arrowtable.Timestamp(name)
		case numeric[i]:
			fields[i] = arrowtable.Float64(name)
		default:
			fields[i] = arrowtable.Utf8(name)
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
			case resp.TimeColumns[i]:
				vals[i] = arrowTimestampMs(v)
			case numeric[i]:
				vals[i] = arrowFloatValue(v)
			default:
				vals[i] = fmt.Sprintf("%v", v)
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

// arrowTimestampMs converts a millisecond-epoch value to a time.Time,
// returning nil for a zero or non-numeric value — mirrors formatTimestampMs'
// treatment of zero as "no timestamp" rather than the Unix epoch instant.
func arrowTimestampMs(v any) any {
	switch val := v.(type) {
	case float64:
		if val == 0 {
			return nil
		}
		return time.UnixMilli(int64(val)).UTC()
	case int64:
		if val == 0 {
			return nil
		}
		return time.UnixMilli(val).UTC()
	default:
		return nil
	}
}

// arrowFloatValue returns v as a float64, or nil if it isn't one.
func arrowFloatValue(v any) any {
	if f, ok := v.(float64); ok {
		return f
	}
	return nil
}

// JSONQueryResponse is the JSON-serializable version of QueryResponse with
// time columns converted from millisecond-epoch integers to RFC3339 strings.
type JSONQueryResponse struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// FormatQueryJSON returns a JSON-serializable copy of resp with time columns
// converted from millisecond-epoch values to RFC3339 strings.
func FormatQueryJSON(resp *QueryResponse) *JSONQueryResponse {
	rows := make([][]any, len(resp.Rows))
	for i, row := range resp.Rows {
		newRow := make([]any, len(row))
		copy(newRow, row)
		for j := range newRow {
			if resp.TimeColumns[j] {
				newRow[j] = formatTimestampMs(newRow[j])
			}
		}
		rows[i] = newRow
	}
	return &JSONQueryResponse{
		Columns: resp.Columns,
		Rows:    rows,
	}
}

// FormatMeasurementsTable formats a MeasurementsResponse as a table.
func FormatMeasurementsTable(w io.Writer, resp *MeasurementsResponse) error {
	if len(resp.Measurements) == 0 {
		fmt.Fprintln(w, "No measurements found")
		return nil
	}

	t := style.NewTable("MEASUREMENT")
	for _, m := range resp.Measurements {
		t.Row(m)
	}

	return t.Render(w)
}

// FormatTagKeysTable formats a TagKeysResponse as a table.
func FormatTagKeysTable(w io.Writer, resp *TagKeysResponse) error {
	if len(resp.TagKeys) == 0 {
		fmt.Fprintln(w, "No tag keys found")
		return nil
	}

	t := style.NewTable("TAG KEY")
	for _, k := range resp.TagKeys {
		t.Row(k)
	}

	return t.Render(w)
}

// FormatTagValuesTable formats a TagValuesResponse as a table.
func FormatTagValuesTable(w io.Writer, resp *TagValuesResponse) error {
	if len(resp.Values) == 0 {
		fmt.Fprintln(w, "No tag values found")
		return nil
	}

	t := style.NewTable("KEY", "VALUE")
	for _, v := range resp.Values {
		t.Row(v.Key, v.Value)
	}

	return t.Render(w)
}

// FormatFieldKeysTable formats a FieldKeysResponse as a table.
func FormatFieldKeysTable(w io.Writer, resp *FieldKeysResponse) error {
	if len(resp.Fields) == 0 {
		fmt.Fprintln(w, "No field keys found")
		return nil
	}

	t := style.NewTable("FIELD KEY", "FIELD TYPE")
	for _, f := range resp.Fields {
		t.Row(f.FieldKey, f.FieldType)
	}

	return t.Render(w)
}
