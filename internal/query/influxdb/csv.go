package influxdb

import "github.com/grafana/gcx/internal/query/dataframe"

func (r *QueryResponse) CSVRecords() ([]string, [][]string, error) {
	return dataframe.CSVRows(r.Columns, r.Rows)
}
