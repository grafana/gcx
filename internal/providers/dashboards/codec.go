package dashboards

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// dashboardTableCodec renders an *unstructured.UnstructuredList (or a slice of
// *unstructured.Unstructured) as a human-readable table.
//
// Default columns: NAME  TITLE  FOLDER  TAGS  AGE
// Wide columns:    NAME  TITLE  FOLDER  TAGS  PANELS  URL  AGE.

// newDashboardTableCodec constructs a dashboardTableCodec.
// wide enables the extended PANELS and URL columns.
// grafanaURL is used to synthesise deep-link URLs in wide mode.
func newDashboardTableCodec(wide bool, grafanaURL string) *dashboardTableCodec {
	return &dashboardTableCodec{Wide: wide, GrafanaURL: grafanaURL}
}

type dashboardTableCodec struct {
	// Wide enables the extra PANELS and URL columns.
	Wide bool

	// GrafanaURL is the base Grafana URL used to synthesise deep-link URLs in
	// wide mode (e.g. "https://mystack.grafana.net"). May be empty when not
	// available, in which case the URL column is left blank.
	GrafanaURL string
}

// Format implements format.Codec.
func (c *dashboardTableCodec) Format() format.Format {
	if c.Wide {
		return "wide"
	}
	return "table"
}

// Decode is a no-op: table format is display-only.
func (c *dashboardTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// Encode writes the table to w.
// It accepts:
//   - *unstructured.UnstructuredList
//   - unstructured.UnstructuredList
//   - *unstructured.Unstructured   (wrapped in a synthetic list)
//   - unstructured.Unstructured
//   - []unstructured.Unstructured
func (c *dashboardTableCodec) Encode(w io.Writer, v any) error {
	items, err := toUnstructuredSlice(v)
	if err != nil {
		return err
	}

	var headers []string
	if c.Wide {
		headers = []string{"NAME", "TITLE", "FOLDER", "TAGS", "PANELS", "URL", "AGE"}
	} else {
		headers = []string{"NAME", "TITLE", "FOLDER", "TAGS", "AGE"}
	}

	t := style.NewTable(headers...)

	for _, item := range items {
		name := item.GetName()
		title := nestedString(item.Object, "spec", "title")
		folder := dashboardFolder(item)
		tags := dashboardTags(item)
		age := dashboardAge(item)

		if c.Wide {
			panels := dashboardPanelCount(item)
			dashURL := dashboardURL(c.GrafanaURL, item)
			t.Row(name, title, folder, tags, panels, dashURL, age)
		} else {
			t.Row(name, title, folder, tags, age)
		}
	}

	return t.Render(w)
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

// dashboardTags returns the dashboard's tags as a comma-separated string.
// Tags live at spec.tags ([]string).
func dashboardTags(item unstructured.Unstructured) string {
	raw, found, err := unstructured.NestedStringSlice(item.Object, "spec", "tags")
	if err != nil || !found {
		return ""
	}
	return strings.Join(raw, ", ")
}

// dashboardAge returns a human-readable age string derived from the resource's
// creationTimestamp. Returns an empty string when the timestamp is missing.
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

// dashboardPanelCount returns the panel count as a string.
// For v1-family dashboards the count comes from spec.panels.
// For v2-family dashboards (grafana-app-sdk) the count comes from spec.elements.
// Returns "" when neither field is present.
func dashboardPanelCount(item unstructured.Unstructured) string {
	apiVersion := item.GetAPIVersion()

	gv, err := schema.ParseGroupVersion(apiVersion)
	isV2Family := err == nil && (gv.Version == "v2" || (strings.HasPrefix(gv.Version, "v2") && len(gv.Version) > 2 && !isDigit(gv.Version[2])))
	if isV2Family {
		elements, found, err := unstructured.NestedSlice(item.Object, "spec", "elements")
		if err != nil || !found {
			return ""
		}
		return strconv.Itoa(len(elements))
	}

	// v1-family (default)
	panels, found, err := unstructured.NestedSlice(item.Object, "spec", "panels")
	if err != nil || !found {
		return ""
	}
	return strconv.Itoa(len(panels))
}

// isDigit reports whether b is an ASCII decimal digit.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// dashboardURL synthesises the Grafana deep-link URL for a dashboard.
// Format: {grafanaURL}/d/{name}/{slug}
// The slug is read from the "grafana.app/slug" annotation.
// Returns empty string when grafanaURL is not set.
func dashboardURL(grafanaURL string, item unstructured.Unstructured) string {
	if grafanaURL == "" {
		return ""
	}

	name := item.GetName()
	slug := ""
	if ann := item.GetAnnotations(); ann != nil {
		slug = ann["grafana.app/slug"]
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

// toUnstructuredSlice normalises the various input types into a []unstructured.Unstructured.
func toUnstructuredSlice(v any) ([]unstructured.Unstructured, error) {
	switch val := v.(type) {
	case *unstructured.UnstructuredList:
		if val == nil {
			return nil, nil
		}
		return val.Items, nil
	case unstructured.UnstructuredList:
		return val.Items, nil
	case *unstructured.Unstructured:
		if val == nil {
			return nil, nil
		}
		return []unstructured.Unstructured{*val}, nil
	case unstructured.Unstructured:
		return []unstructured.Unstructured{val}, nil
	case []unstructured.Unstructured:
		return val, nil
	default:
		return nil, fmt.Errorf("dashboardTableCodec: unsupported type %T", v)
	}
}
