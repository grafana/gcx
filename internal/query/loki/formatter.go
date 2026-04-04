package loki

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/grafana/gcx/internal/style"
)

func FormatQueryTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Data.Result) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	for _, stream := range resp.Data.Result {
		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}
			fmt.Fprintln(w, value[1])
		}
	}

	return nil
}

func FormatQueryTableWide(w io.Writer, resp *QueryResponse) error {
	if len(resp.Data.Result) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	labelNames := collectStreamLabelNames(resp.Data.Result)

	header := make([]string, 0, len(labelNames)+1)
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	header = append(header, "LINE")
	t := style.NewTable(header...)

	for _, stream := range resp.Data.Result {
		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}

			row := make([]string, 0, len(labelNames)+1)
			for _, name := range labelNames {
				if val, ok := stream.Stream[name]; ok {
					row = append(row, val)
				} else {
					row = append(row, "")
				}
			}
			row = append(row, value[1])
			t.Row(row...)
		}
	}

	return t.Render(w)
}

func FormatLabelsTable(w io.Writer, resp *LabelsResponse) error {
	t := style.NewTable("LABEL")
	for _, label := range resp.Data {
		t.Row(label)
	}
	return t.Render(w)
}

func FormatSeriesTable(w io.Writer, resp *SeriesResponse) error {
	if len(resp.Data) == 0 {
		fmt.Fprintln(w, "No series found")
		return nil
	}

	labelNames := collectLabelNames(resp.Data)

	header := make([]string, 0, len(labelNames))
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	t := style.NewTable(header...)

	for _, series := range resp.Data {
		row := make([]string, 0, len(labelNames))
		for _, name := range labelNames {
			if val, ok := series[name]; ok {
				row = append(row, val)
			} else {
				row = append(row, "")
			}
		}
		t.Row(row...)
	}

	return t.Render(w)
}

// FormatMetricQueryTable formats a MetricQueryResponse as a table with TIMESTAMP, VALUE, and label columns.
func FormatMetricQueryTable(w io.Writer, resp *MetricQueryResponse) error {
	if len(resp.Data.Result) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	labelNames := collectMetricLabelNames(resp.Data.Result)

	header := make([]string, 0, len(labelNames)+2)
	header = append(header, "TIMESTAMP", "VALUE")
	for _, name := range labelNames {
		header = append(header, strings.ToUpper(name))
	}
	t := style.NewTable(header...)

	for _, sample := range resp.Data.Result {
		if len(sample.Values) > 0 {
			for _, v := range sample.Values {
				if len(v) < 2 {
					continue
				}
				row := make([]string, 0, len(labelNames)+2)
				row = append(row, fmt.Sprintf("%v", v[0]), fmt.Sprintf("%v", v[1]))
				for _, name := range labelNames {
					row = append(row, sample.Metric[name])
				}
				t.Row(row...)
			}
		} else if len(sample.Value) >= 2 {
			row := make([]string, 0, len(labelNames)+2)
			row = append(row, fmt.Sprintf("%v", sample.Value[0]), fmt.Sprintf("%v", sample.Value[1]))
			for _, name := range labelNames {
				row = append(row, sample.Metric[name])
			}
			t.Row(row...)
		}
	}

	return t.Render(w)
}

func collectMetricLabelNames(samples []MetricQuerySample) []string {
	nameSet := make(map[string]struct{})
	for _, s := range samples {
		for name := range s.Metric {
			if !isHiddenLabel(name) {
				nameSet[name] = struct{}{}
			}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func collectStreamLabelNames(streams []StreamEntry) []string {
	nameSet := make(map[string]struct{})
	for _, stream := range streams {
		for name := range stream.Stream {
			if !isHiddenLabel(name) {
				nameSet[name] = struct{}{}
			}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

func isHiddenLabel(name string) bool {
	return strings.HasPrefix(name, "__")
}

func collectLabelNames(series []map[string]string) []string {
	nameSet := make(map[string]struct{})
	for _, s := range series {
		for name := range s {
			nameSet[name] = struct{}{}
		}
	}

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}
