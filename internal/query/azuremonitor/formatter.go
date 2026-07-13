package azuremonitor

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
			if i >= len(frame.Values) {
				break
			}
			val := formatPtrValue(frame.Values[i])
			t.Row(ts.Format("2006-01-02T15:04:05Z07:00"), val, label)
		}
	}
	return t.Render(w)
}

// FormatWide renders query results as a wide table with UNIT and LABEL columns.
func FormatWide(w io.Writer, resp *QueryResponse) error {
	if len(resp.Frames) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}

	t := style.NewTable("TIMESTAMP", "VALUE", "UNIT", "SERIES", "LABEL")
	for _, frame := range resp.Frames {
		label := frameLabel(frame)
		labelStr := formatLabelsMap(frame.Labels)
		for i, ts := range frame.Timestamps {
			if i >= len(frame.Values) {
				break
			}
			val := formatPtrValue(frame.Values[i])
			t.Row(ts.Format("2006-01-02T15:04:05Z07:00"), val, frame.Unit, label, labelStr)
		}
	}
	return t.Render(w)
}

// FormatSubscriptions renders a list of Azure subscriptions.
func FormatSubscriptions(w io.Writer, subs []Subscription) error {
	if len(subs) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("SUBSCRIPTION ID", "NAME")
	for _, s := range subs {
		t.Row(s.ID, s.Name)
	}
	return t.Render(w)
}

// FormatResourceGroups renders a list of Azure resource groups.
func FormatResourceGroups(w io.Writer, groups []ResourceGroup) error {
	if len(groups) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("NAME", "LOCATION")
	for _, g := range groups {
		t.Row(g.Name, g.Location)
	}
	return t.Render(w)
}

// FormatResources renders a list of Azure resources.
func FormatResources(w io.Writer, resources []Resource) error {
	if len(resources) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("NAME", "TYPE", "LOCATION")
	for _, r := range resources {
		t.Row(r.Name, r.Type, r.Location)
	}
	return t.Render(w)
}

// FormatMetricDefinitions renders a list of Azure Monitor metric definitions.
func FormatMetricDefinitions(w io.Writer, defs []MetricDefinition) error {
	if len(defs) == 0 {
		fmt.Fprintln(w, "No data")
		return nil
	}
	t := style.NewTable("METRIC", "AGGREGATION", "UNIT", "DIMENSIONS")
	for _, d := range defs {
		t.Row(d.Name, d.PrimaryAggregation, d.Unit, strings.Join(d.Dimensions, ","))
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
