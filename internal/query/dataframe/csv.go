package dataframe

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"
)

// CSVRows preserves strings and serializes structured cells as JSON. Time columns
// retain their query response representation, including epoch milliseconds.
func CSVRows(columns []string, values [][]any) ([]string, [][]string, error) {
	rows := make([][]string, 0, len(values))
	for _, values := range values {
		row := make([]string, len(values))
		for i, value := range values {
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

// CSVSeries is the common export projection of parsed metric frames.
type CSVSeries struct {
	Name       string
	Unit       string
	Labels     map[string]string
	Timestamps []time.Time
	Values     []*float64
}

func CSVSeriesRecords(series []CSVSeries, includeUnit bool) ([]string, [][]string, error) {
	keys := map[string]bool{}
	for _, s := range series {
		for key := range s.Labels {
			keys[key] = true
		}
	}
	labels := make([]string, 0, len(keys))
	for key := range keys {
		labels = append(labels, key)
	}
	slices.Sort(labels)
	columns := []string{"timestamp", "value", "series"}
	if includeUnit {
		columns = append(columns, "unit")
	}
	for _, label := range labels {
		columns = append(columns, "label."+label)
	}
	var rows [][]string
	for _, s := range series {
		if len(s.Timestamps) != len(s.Values) {
			return nil, nil, fmt.Errorf("csv series %q: timestamps and values have different lengths", s.Name)
		}
		for i, timestamp := range s.Timestamps {
			value := ""
			if s.Values[i] != nil {
				value = strconv.FormatFloat(*s.Values[i], 'f', -1, 64)
			}
			row := []string{timestamp.UTC().Format(time.RFC3339Nano), value, s.Name}
			if includeUnit {
				row = append(row, s.Unit)
			}
			for _, label := range labels {
				row = append(row, s.Labels[label])
			}
			rows = append(rows, row)
		}
	}
	return columns, rows, nil
}
