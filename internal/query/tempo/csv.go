package tempo

import (
	"slices"
	"strconv"

	"github.com/grafana/gcx/internal/query/dataframe"
)

func (r *SearchResponse) CSVRecords() ([]string, [][]string, error) {
	rows := make([][]any, 0, len(r.Traces))
	for _, t := range r.Traces {
		rows = append(rows, []any{t.TraceID, t.RootServiceName, t.RootTraceName, t.StartTimeUnixNano, t.DurationMs, t.ServiceStats})
	}
	return dataframe.CSVRows([]string{"trace_id", "root_service", "root_trace", "start_time_unix_nano", "duration_ms", "service_stats"}, rows)
}

func (r BaselineResult) CSVRecords() ([]string, [][]string, error) {
	rows := make([][]any, 0, len(r.Candidates))
	for _, t := range r.Candidates {
		rows = append(rows, []any{r.SeedTraceID, t.TraceID, t.RootServiceName, t.RootTraceName, t.StartTimeUnixNano, t.DurationMs, t.SpanCount, t.ServiceCount, t.ErrorCount})
	}
	return dataframe.CSVRows([]string{"seed_trace_id", "trace_id", "root_service", "root_trace", "start_time_unix_nano", "duration_ms", "span_count", "service_count", "error_count"}, rows)
}

func (r *MetricsResponse) CSVRecords() ([]string, [][]string, error) {
	keys := map[string]bool{}
	for _, s := range r.Series {
		for _, label := range s.Labels {
			keys[label.Key] = true
		}
	}
	labels := make([]string, 0, len(keys))
	for key := range keys {
		labels = append(labels, key)
	}
	slices.Sort(labels)
	columns := make([]string, 0, 2+len(labels))
	if !r.Instant {
		columns = append(columns, "timestamp_unix_ms")
	}
	columns = append(columns, "value")
	for _, label := range labels {
		columns = append(columns, "label."+label)
	}
	var rows [][]string
	for _, s := range r.Series {
		values := map[string]string{}
		for _, label := range s.Labels {
			values[label.Key] = extractLabelValue(label.Value)
		}
		samples := s.Samples
		if len(samples) == 0 && s.Value != nil {
			samples = []MetricsSample{{TimestampMs: s.TimestampMs, Value: *s.Value}}
		}
		for _, sample := range samples {
			row := make([]string, 0, len(columns))
			if !r.Instant {
				row = append(row, sample.TimestampMs)
			}
			row = append(row, strconv.FormatFloat(sample.Value, 'f', -1, 64))
			for _, label := range labels {
				row = append(row, values[label])
			}
			rows = append(rows, row)
		}
	}
	return columns, rows, nil
}
