package prometheus

import "fmt"

// CSVRecords preserves sample values and Unix-second timestamps without the
// terminal formatter's rounding. Prefixes keep labels distinct from data fields.
func (resp *QueryResponse) CSVRecords() ([]string, [][]string, error) {
	labels := collectLabelNames(resp.Data.Result)
	columns := make([]string, 0, 2+len(labels))
	columns = append(columns, "timestamp_unix_seconds", "value")
	for _, label := range labels {
		columns = append(columns, "label."+label)
	}
	var rows [][]string
	for _, sample := range resp.Data.Result {
		points := sample.Values
		if len(points) == 0 && len(sample.Value) > 0 {
			points = [][]any{sample.Value}
		}
		for _, point := range points {
			if len(point) < 2 {
				return nil, nil, fmt.Errorf("invalid metric sample: expected timestamp and value, got %d fields", len(point))
			}
			row := []string{parseValue(point[0]), parseValue(point[1])}
			for _, label := range labels {
				row = append(row, sample.Metric[label])
			}
			rows = append(rows, row)
		}
	}
	return columns, rows, nil
}
