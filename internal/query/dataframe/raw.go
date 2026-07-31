package dataframe

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/grafana/gcx/internal/style"
)

// RawQueryResponse holds the result of a raw (plugin-agnostic) datasource
// query. Each Grafana data frame becomes a separate RawFrame so multi-frame
// responses are preserved end-to-end.
type RawQueryResponse struct {
	Frames []RawFrame `json:"frames"`
}

// RawFrame is a single data frame converted from column-oriented to
// row-oriented layout.
type RawFrame struct {
	Name    string      `json:"name,omitempty"`
	Columns []RawColumn `json:"columns"`
	Rows    [][]any     `json:"rows"`
}

// RawColumn describes a single column in a raw query result.
type RawColumn struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// ConvertResponse converts a Grafana data-frame Response into a
// RawQueryResponse by pivoting every frame for the given refId from
// column-oriented to row-oriented layout.
func ConvertResponse(resp *Response, refId string) *RawQueryResponse {
	out := &RawQueryResponse{
		Frames: []RawFrame{},
	}

	result, ok := resp.Results[refId]
	if !ok {
		return out
	}

	for _, frame := range result.Frames {
		if len(frame.Schema.Fields) == 0 {
			continue
		}

		rf := RawFrame{
			Name:    frame.Schema.Name,
			Columns: make([]RawColumn, len(frame.Schema.Fields)),
			Rows:    [][]any{},
		}

		for i, field := range frame.Schema.Fields {
			rf.Columns[i] = RawColumn{Name: field.Name, Type: field.Type}
		}

		if len(frame.Data.Values) > 0 && len(frame.Data.Values[0]) > 0 {
			numFields := len(frame.Schema.Fields)
			numRows := len(frame.Data.Values[0])
			for i := range numRows {
				row := make([]any, numFields)
				for colIdx, colValues := range frame.Data.Values {
					if colIdx < numFields && i < len(colValues) {
						row[colIdx] = colValues[i]
					}
				}
				rf.Rows = append(rf.Rows, row)
			}
		}

		out.Frames = append(out.Frames, rf)
	}

	return out
}

// FormatTable renders a RawQueryResponse as terminal tables.
// Each frame is rendered as a separate table, with a heading when
// there are multiple frames.
func FormatTable(w io.Writer, resp *RawQueryResponse) error {
	totalRows := 0
	for _, f := range resp.Frames {
		totalRows += len(f.Rows)
	}
	if totalRows == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	multiFrame := len(resp.Frames) > 1

	for i, frame := range resp.Frames {
		if len(frame.Columns) == 0 {
			continue
		}

		if multiFrame {
			if i > 0 {
				fmt.Fprintln(w)
			}
			label := frame.Name
			if label == "" {
				label = fmt.Sprintf("Frame %d", i+1)
			}
			fmt.Fprintf(w, "── %s (%d rows) ──\n", label, len(frame.Rows))
		}

		headers := make([]string, len(frame.Columns))
		for j, col := range frame.Columns {
			headers[j] = strings.ToUpper(col.Name)
		}

		t := style.NewTable(headers...)
		for _, row := range frame.Rows {
			cells := make([]string, len(row))
			for j, val := range row {
				cells[j] = toString(val)
			}
			t.Row(cells...)
		}

		if err := t.Render(w); err != nil {
			return err
		}
	}

	return nil
}

func toString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		return strconv.FormatBool(val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}
