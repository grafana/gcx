package dataframe

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// CSVRows preserves strings and serializes structured cells as JSON. Time columns
// retain their query response representation, including epoch milliseconds.
func CSVRows(columns []string, values [][]any) ([]string, [][]string, error) {
	rows := make([][]string, 0, len(values))
	for _, cells := range values {
		row := make([]string, len(cells))
		for i, value := range cells {
			switch v := value.(type) {
			case nil:
			case string:
				row[i] = v
			case float64:
				row[i] = strconv.FormatFloat(v, 'f', -1, 64)
			default:
				b, err := json.Marshal(v)
				if err != nil {
					return nil, nil, fmt.Errorf("csv column %d: %w", i+1, err)
				}
				if string(b) != "null" {
					row[i] = string(b)
				}
			}
		}
		rows = append(rows, row)
	}
	return columns, rows, nil
}
