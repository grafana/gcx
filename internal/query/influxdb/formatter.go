package influxdb

import (
	"fmt"
	"io"
	"strconv"
	"time"

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
