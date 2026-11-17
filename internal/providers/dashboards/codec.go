package dashboards

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	cmdio "github.com/grafana/gcx/internal/output"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func dashboardTable(grafanaURL string) cmdio.Table[unstructured.Unstructured] {
	return cmdio.Table[unstructured.Unstructured]{Columns: []cmdio.Column[unstructured.Unstructured]{
		{Header: "NAME", Content: func(item unstructured.Unstructured) string { return item.GetName() }},
		{Header: "TITLE", Content: func(item unstructured.Unstructured) string { return nestedString(item.Object, "spec", "title") }},
		{Header: "FOLDER", Content: dashboardFolder},
		{Header: "TAGS", Content: dashboardTags},
		{Header: "PANELS", Visible: cmdio.WideOnly, Content: dashboardPanelCount},
		{Header: "URL", Visible: cmdio.WideOnly, Content: func(item unstructured.Unstructured) string { return dashboardURL(grafanaURL, item) }},
		{Header: "AGE", Content: dashboardAge},
	}}
}

func dashboardListTable(grafanaURL string) cmdio.TableAdapter[*unstructured.UnstructuredList, unstructured.Unstructured] {
	return cmdio.TableAdapter[*unstructured.UnstructuredList, unstructured.Unstructured]{Table: dashboardTable(grafanaURL), Rows: func(list *unstructured.UnstructuredList) ([]unstructured.Unstructured, error) {
		if list == nil {
			return nil, nil
		}
		return list.Items, nil
	}}
}

func dashboardGetTable(grafanaURL string) cmdio.TableAdapter[*unstructured.Unstructured, unstructured.Unstructured] {
	return cmdio.TableAdapter[*unstructured.Unstructured, unstructured.Unstructured]{Table: dashboardTable(grafanaURL), Rows: func(item *unstructured.Unstructured) ([]unstructured.Unstructured, error) {
		if item == nil {
			return nil, nil
		}
		return []unstructured.Unstructured{*item}, nil
	}}
}

// dashboardFolder extracts the folder name from the dashboard's annotations.
// The annotation key is "grafana.app/folder"; empty values map to "General".
func dashboardFolder(item unstructured.Unstructured) string {
	annotations := item.GetAnnotations()
	if annotations == nil {
		return "General"
	}
	folder := annotations["grafana.app/folder"]
	if folder == "" {
		return "General"
	}
	return folder
}

func dashboardTags(item unstructured.Unstructured) string {
	return strings.Join(dashboardTagSlice(item), ", ")
}

func dashboardTagSlice(item unstructured.Unstructured) []string {
	raw, found, err := unstructured.NestedStringSlice(item.Object, "spec", "tags")
	if err != nil || !found {
		return nil
	}
	return raw
}

func dashboardAge(item unstructured.Unstructured) string {
	ts := item.GetCreationTimestamp()
	if ts.IsZero() {
		return ""
	}
	return formatAge(time.Since(ts.Time))
}

// formatAge converts a duration into a compact human-readable string:
// "Xs" (seconds), "Xm" (minutes), "Xh" (hours), "Xd" (days).
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func dashboardPanelCount(item unstructured.Unstructured) string {
	count := dashboardPanelCountValue(item)
	if count == nil {
		return ""
	}
	return strconv.Itoa(*count)
}

// dashboardPanelCountValue returns the panel count.
// For v1-family dashboards the count comes from spec.panels.
// For v2-family dashboards (grafana-app-sdk) the count comes from spec.elements.
// Returns nil when neither field is present.
func dashboardPanelCountValue(item unstructured.Unstructured) *int {
	apiVersion := item.GetAPIVersion()

	gv, err := schema.ParseGroupVersion(apiVersion)
	isV2Family := err == nil && (gv.Version == "v2" || (strings.HasPrefix(gv.Version, "v2") && len(gv.Version) > 2 && !isDigit(gv.Version[2])))
	if isV2Family {
		// v2 spec.elements is a map[id]→element, not a slice.
		elements, found, err := unstructured.NestedMap(item.Object, "spec", "elements")
		if err != nil || !found {
			return nil
		}
		count := len(elements)
		return &count
	}

	// v1-family (default)
	panels, found, err := unstructured.NestedSlice(item.Object, "spec", "panels")
	if err != nil || !found {
		return nil
	}
	count := len(panels)
	return &count
}

// isDigit reports whether b is an ASCII decimal digit.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func dashboardURL(grafanaURL string, item unstructured.Unstructured) string {
	slug := ""
	if ann := item.GetAnnotations(); ann != nil {
		slug = ann["grafana.app/slug"]
	}
	return dashboardURLFromParts(grafanaURL, item.GetName(), slug)
}

func dashboardURLFromParts(grafanaURL, name, slug string) string {
	if grafanaURL == "" {
		return ""
	}

	base := strings.TrimSuffix(grafanaURL, "/")
	if slug != "" {
		return fmt.Sprintf("%s/d/%s/%s", base, name, slug)
	}
	return fmt.Sprintf("%s/d/%s", base, name)
}

// nestedString is a nil-safe helper to extract a string from a nested map path.
func nestedString(obj map[string]any, fields ...string) string {
	s, found, err := unstructured.NestedString(obj, fields...)
	if err != nil || !found {
		return ""
	}
	return s
}
