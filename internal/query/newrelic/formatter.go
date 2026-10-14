package newrelic

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/query/dataframe"
	"github.com/grafana/gcx/internal/style"
)

// FormatTable renders each frame as a separate row-oriented table.
func FormatTable(w io.Writer, resp *QueryResponse) error {
	count := 0
	for i, frame := range resp.Frames {
		rows := frameRows(frame)
		if rows == 0 || len(frame.Schema.Fields) == 0 {
			continue
		}
		if _, err := fmt.Fprintln(w, frameName(frame, i)); err != nil {
			return err
		}
		headers := make([]string, len(frame.Schema.Fields))
		for col, field := range frame.Schema.Fields {
			headers[col] = strings.ToUpper(field.Name)
		}
		table := style.NewTable(headers...)
		for row := range rows {
			values := make([]string, len(headers))
			for col := range headers {
				values[col] = cell(frame, col, row)
			}
			table.Row(values...)
		}
		if err := table.Render(w); err != nil {
			return err
		}
		count++
	}
	if count == 0 {
		_, err := fmt.Fprintln(w, "No data")
		return err
	}
	return nil
}

// FormatWide renders the same row-oriented tables as FormatTable.
func FormatWide(w io.Writer, resp *QueryResponse) error {
	return FormatTable(w, resp)
}

func frameRows(frame dataframe.Frame) int {
	rows := 0
	for _, col := range frame.Data.Values {
		rows = max(rows, len(col))
	}
	return rows
}

func frameName(frame dataframe.Frame, i int) string {
	if frame.Schema.Name != "" {
		return frame.Schema.Name
	}
	return fmt.Sprintf("Frame %d", i+1)
}

func cell(frame dataframe.Frame, col, row int) string {
	if col >= len(frame.Data.Values) || row >= len(frame.Data.Values[col]) || frame.Data.Values[col][row] == nil {
		return ""
	}
	value := frame.Data.Values[col][row]
	if n, ok := value.(float64); ok {
		if col < len(frame.Schema.Fields) && frame.Schema.Fields[col].Type == "time" {
			return time.UnixMilli(int64(n)).UTC().Format(time.RFC3339Nano)
		}
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}
