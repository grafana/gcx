package azuremonitor

import "github.com/grafana/gcx/internal/query/dataframe"

func (r *TableResponse) CSVRecords() ([]string, [][]string, error) {
	columns := make([]string, len(r.Columns))
	for i, column := range r.Columns {
		columns[i] = column.Name
	}
	return dataframe.CSVRows(columns, r.Rows)
}

func (r *QueryResponse) CSVRecords() ([]string, [][]string, error) {
	series := make([]dataframe.CSVSeries, 0, len(r.Frames))
	for _, frame := range r.Frames {
		series = append(series, dataframe.CSVSeries{Name: frame.Name, Timestamps: frame.Timestamps, Values: frame.Values, Labels: frame.Labels, Unit: frame.Unit})
	}
	return dataframe.CSVSeriesRecords(series, true)
}
