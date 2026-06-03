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

// dashboardTableCodec renders dashboard summary data as a human-readable table.
// Raw unstructured Dashboard objects are projected to summaries before rendering.
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
// It accepts dashboardSummaryList values plus raw unstructured Dashboard objects
// and lists, which are projected to summaries before rendering.
func (c *dashboardTableCodec) Encode(w io.Writer, v any) error {
	list, err := toDashboardSummaryList(v)
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

	for _, item := range list.Items {
		name := item.Metadata.Name
		title := item.Spec.Title
		folder := item.Spec.Folder
		if folder == "" {
			folder = "General"
		}
		tags := strings.Join(item.Spec.Tags, ", ")
		age := dashboardSummaryAge(item)

		if c.Wide {
			panels := dashboardSummaryPanelCount(item)
			dashURL := dashboardSummaryURL(c.GrafanaURL, item)
			t.Row(name, title, folder, tags, panels, dashURL, age)
		} else {
			t.Row(name, title, folder, tags, age)
		}
	}

	return t.Render(w)
}

func toDashboardSummaryList(v any) (*dashboardSummaryList, error) {
	switch val := v.(type) {
	case *dashboardSummaryList:
		return val, nil
	case dashboardSummaryList:
		return &val, nil
	default:
		items, err := toUnstructuredSlice(v)
		if err != nil {
			return nil, err
		}
		return dashboardListSummary(&unstructured.UnstructuredList{Items: items}), nil
	}
}

func dashboardSummaryAge(item dashboardSummary) string {
	if item.Metadata.CreationTimestamp == nil || item.Metadata.CreationTimestamp.IsZero() {
		return ""
	}
	return formatAge(time.Since(item.Metadata.CreationTimestamp.Time))
}

func dashboardSummaryPanelCount(item dashboardSummary) string {
	if item.Spec.PanelCount == nil {
		return ""
	}
	return strconv.Itoa(*item.Spec.PanelCount)
}

func dashboardSummaryURL(grafanaURL string, item dashboardSummary) string {
	slug := ""
	if item.Metadata.Annotations != nil {
		slug = item.Metadata.Annotations["grafana.app/slug"]
	}
	return dashboardURLFromParts(grafanaURL, item.Metadata.Name, slug)
}

func dashboardFolderUID(item unstructured.Unstructured) string {
	annotations := item.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return annotations["grafana.app/folder"]
}

func dashboardFolderPath(folderUID string, folderPaths map[string]string) string {
	if folderUID == "" {
		return "General"
	}
	if path := folderPaths[folderUID]; path != "" {
		return path
	}
	return folderUID
}

func dashboardTagSlice(item unstructured.Unstructured) []string {
	raw, found, err := unstructured.NestedStringSlice(item.Object, "spec", "tags")
	if err != nil || !found {
		return nil
	}
	return raw
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
