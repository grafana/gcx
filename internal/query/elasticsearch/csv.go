package elasticsearch

import "github.com/grafana/gcx/internal/query/dataframe"

func (r *MetricsResponse) CSVRecords() ([]string, [][]string, error) {
	series := make([]dataframe.CSVSeries, 0, len(r.Series))
	for _, frame := range r.Series {
		series = append(series, dataframe.CSVSeries{Name: frame.Name, Timestamps: frame.Timestamps, Values: frame.Values})
	}
	return dataframe.CSVSeriesRecords(series, false)
}
