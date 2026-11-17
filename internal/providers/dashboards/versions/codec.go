// Package versions provides the `gcx dashboards list-versions` command and
// the `gcx dashboards versions` command group.
package versions

import (
	"strconv"

	cmdio "github.com/grafana/gcx/internal/output"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func versionsTable() cmdio.TableAdapter[*unstructured.UnstructuredList, unstructured.Unstructured] {
	return cmdio.TableAdapter[*unstructured.UnstructuredList, unstructured.Unstructured]{
		Table: cmdio.Table[unstructured.Unstructured]{Columns: []cmdio.Column[unstructured.Unstructured]{
			{Header: "VERSION", Content: func(item unstructured.Unstructured) string { return strconv.FormatInt(item.GetGeneration(), 10) }},
			{Header: "TIMESTAMP", Content: func(item unstructured.Unstructured) string {
				return annotationOrEmpty(item, "grafana.app/updatedTimestamp")
			}},
			{Header: "AUTHOR", Content: func(item unstructured.Unstructured) string { return annotationOrEmpty(item, "grafana.app/updatedBy") }},
			{Header: "MESSAGE", Content: func(item unstructured.Unstructured) string { return annotationOrEmpty(item, "grafana.app/message") }},
		}},
		Rows: func(list *unstructured.UnstructuredList) ([]unstructured.Unstructured, error) {
			if list == nil {
				return nil, nil
			}
			return list.Items, nil
		},
	}
}

// annotationOrEmpty returns the value of the named annotation, or "" if absent.
func annotationOrEmpty(item unstructured.Unstructured, key string) string {
	ann := item.GetAnnotations()
	if ann == nil {
		return ""
	}
	return ann[key]
}
