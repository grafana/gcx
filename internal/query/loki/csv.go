package loki

import (
	"slices"

	"github.com/grafana/gcx/internal/query/prometheus"
)

// CSVRecords keeps original lines and the three label namespaces separate;
// parsing messages for terminal display would discard information on export.
func (resp *QueryResponse) CSVRecords() ([]string, [][]string, error) {
	keys := make(map[string]struct{})
	for _, stream := range resp.Data.Result {
		for key := range stream.Stream {
			keys["label."+key] = struct{}{}
		}
		for _, entry := range stream.Values {
			for key := range entry.StructuredMetadata {
				keys["metadata."+key] = struct{}{}
			}
			for key := range entry.Parsed {
				keys["parsed."+key] = struct{}{}
			}
		}
	}
	labels := make([]string, 0, len(keys))
	for key := range keys {
		labels = append(labels, key)
	}
	slices.Sort(labels)
	columns := append([]string{"timestamp_unix_nano", "message"}, labels...)
	positions := make(map[string]int, len(columns))
	for i, column := range columns {
		positions[column] = i
	}
	var rows [][]string
	for _, stream := range resp.Data.Result {
		for _, entry := range stream.Values {
			row := make([]string, len(columns))
			row[0], row[1] = entry.Timestamp, entry.Line
			for key, value := range stream.Stream {
				row[positions["label."+key]] = value
			}
			for key, value := range entry.StructuredMetadata {
				row[positions["metadata."+key]] = value
			}
			for key, value := range entry.Parsed {
				row[positions["parsed."+key]] = value
			}
			rows = append(rows, row)
		}
	}
	return columns, rows, nil
}

func (resp *MetricQueryResponse) CSVRecords() ([]string, [][]string, error) {
	samples := make([]prometheus.Sample, len(resp.Data.Result))
	for i, sample := range resp.Data.Result {
		samples[i] = prometheus.Sample(sample)
	}
	return (&prometheus.QueryResponse{Data: prometheus.ResultData{
		ResultType: resp.Data.ResultType,
		Result:     samples,
	}}).CSVRecords()
}
