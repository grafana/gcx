package cloudwatch

import "github.com/grafana/gcx/internal/query/dataframe"

func (r *QueryResponse) CSVRecords() ([]string, [][]string, error) {
	series := make([]dataframe.CSVSeries, 0, len(r.Frames))
	for _, frame := range r.Frames {
		series = append(series, dataframe.CSVSeries{Name: frame.Name, Timestamps: frame.Timestamps, Values: frame.Values, Labels: frame.Labels})
	}
	return dataframe.CSVSeriesRecords(series, false)
}
