package pyroscope

import (
	"cmp"
	"slices"
	"strconv"
)

func (r *QueryResponse) CSVRecords() ([]string, [][]string, error) {
	columns := []string{"function", "self", "total", "self_percentage"}
	if r.Flamegraph == nil {
		return columns, nil, nil
	}
	// The name count bounds the distinct functions without the terminal's top-20 cap.
	functions := ExtractTopFunctions(r.Flamegraph, len(r.Flamegraph.Names))
	slices.SortFunc(functions, func(a, b FunctionSample) int {
		if order := cmp.Compare(b.Self, a.Self); order != 0 {
			return order
		}
		return cmp.Compare(a.Name, b.Name)
	})
	rows := make([][]string, 0, len(functions))
	for _, f := range functions {
		rows = append(rows, []string{f.Name, strconv.FormatInt(f.Self, 10), strconv.FormatInt(f.Total, 10), strconv.FormatFloat(f.Percentage, 'f', -1, 64)})
	}
	return columns, rows, nil
}
