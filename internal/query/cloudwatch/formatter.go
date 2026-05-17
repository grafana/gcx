package cloudwatch

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

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
			val := formatPtrValue(frame.Values[i])
			t.Row(ts.Format("2006-01-02T15:04:05Z07:00"), val, label)
		}
	}
	return t.Render(w)
}

// FormatWide renders query results as a wide table with a LABEL column.
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
			val := formatPtrValue(frame.Values[i])
			t.Row(ts.Format("2006-01-02T15:04:05Z07:00"), val, label, labelStr)
		}
	}
	return t.Render(w)
}

// FormatNamespaces renders a list of CloudWatch namespaces.
func FormatNamespaces(w io.Writer, namespaces []string) error {
	if len(namespaces) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("NAMESPACE")
	for _, ns := range namespaces {
		t.Row(ns)
	}
	return t.Render(w)
}

// FormatMetrics renders a list of CloudWatch metrics.
func FormatMetrics(w io.Writer, metrics []Metric) error {
	if len(metrics) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("NAMESPACE", "METRIC")
	for _, m := range metrics {
		t.Row(m.Namespace, m.Name)
	}
	return t.Render(w)
}

// FormatDimensions renders a list of CloudWatch dimension keys.
func FormatDimensions(w io.Writer, keys []string) error {
	if len(keys) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("DIMENSION")
	for _, k := range keys {
		t.Row(k)
	}
	return t.Render(w)
}

// FormatRegions renders a list of AWS regions.
func FormatRegions(w io.Writer, regions []string) error {
	if len(regions) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("REGION")
	for _, r := range regions {
		t.Row(r)
	}
	return t.Render(w)
}

// FormatAccounts renders a list of AWS accounts.
func FormatAccounts(w io.Writer, accounts []Account) error {
	if len(accounts) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("ID", "LABEL", "ARN")
	for _, a := range accounts {
		t.Row(a.ID, a.Label, a.ARN)
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
