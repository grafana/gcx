package sql

import "github.com/grafana/gcx/internal/query/dataframe"

func (r *QueryResponse) CSVRecords() ([]string, [][]string, error) {
	columns := make([]string, len(r.Columns))
	for i, column := range r.Columns {
		columns[i] = column.Name
	}
	return dataframe.CSVRows(columns, r.Rows)
}
