package cloudmonitoring

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/style"
)

// FormatTable renders query results as a compact table.
func FormatTable(w io.Writer, resp *QueryResponse) error {
	if len(resp.Frames) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	t := style.NewTable("TIMESTAMP", "VALUE", "SERIES")
	for _, frame := range resp.Frames {
		label := frameLabel(frame)
		for i, ts := range frame.Timestamps {
			if i >= len(frame.Values) {
				break
			}
			t.Row(ts.Format(time.RFC3339), formatPtrValue(frame.Values[i]), label)
		}
	}
	return t.Render(w)
}

// FormatWide renders query results with a LABEL column carrying all series labels.
func FormatWide(w io.Writer, resp *QueryResponse) error {
	if len(resp.Frames) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	t := style.NewTable("TIMESTAMP", "VALUE", "SERIES", "LABEL")
	for _, frame := range resp.Frames {
		label := frameLabel(frame)
		labelStr := formatLabelsMap(frame.Labels)
		for i, ts := range frame.Timestamps {
			if i >= len(frame.Values) {
				break
			}
			t.Row(ts.Format(time.RFC3339), formatPtrValue(frame.Values[i]), label, labelStr)
		}
	}
	return t.Render(w)
}

// FormatProjects renders the project listing.
func FormatProjects(w io.Writer, projects []Project) error {
	if len(projects) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("PROJECT ID", "NAME")
	for _, p := range projects {
		t.Row(p.ID, p.Name)
	}
	return t.Render(w)
}

// FormatMetricDescriptors renders metric descriptors.
func FormatMetricDescriptors(w io.Writer, descriptors []MetricDescriptor) error {
	if len(descriptors) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("METRIC TYPE", "KIND", "VALUE", "UNIT")
	for _, d := range descriptors {
		t.Row(d.Type, d.MetricKind, d.ValueType, d.Unit)
	}
	return t.Render(w)
}

func frameLabel(frame Frame) string {
	if frame.Name != "" {
		return frame.Name
	}
	return formatLabelsMap(frame.Labels)
}

func formatLabelsMap(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strconv.Quote(labels[k]))
	}
	return b.String()
}

func formatPtrValue(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}
