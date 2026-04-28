// Package versions provides the `gcx dashboards versions` command group.
package versions

import (
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/grafana/gcx/internal/format"
	"github.com/grafana/gcx/internal/style"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// versionsTableCodec renders dashboard revision history as a human-readable table.
//
// Columns: VERSION TIMESTAMP AUTHOR MESSAGE
//
//   - VERSION   = metadata.generation (integer, rendered as string)
//   - TIMESTAMP = annotation grafana.app/updatedTimestamp (raw; NOT metadata.creationTimestamp)
//   - AUTHOR    = annotation grafana.app/updatedBy
//   - MESSAGE   = annotation grafana.app/message
//
// Missing annotations render as empty strings, never "<nil>" or errors.
type versionsTableCodec struct{}

// Format implements format.Codec.
func (c *versionsTableCodec) Format() format.Format { return "table" }

// Decode is a no-op: table format is display-only.
func (c *versionsTableCodec) Decode(_ io.Reader, _ any) error {
	return errors.New("table format does not support decoding")
}

// Encode writes the versions table to w.
// It accepts []unstructured.Unstructured (the slice returned after sorting
// history items by descending generation).
func (c *versionsTableCodec) Encode(w io.Writer, v any) error {
	items, err := toVersionsSlice(v)
	if err != nil {
		return err
	}

	t := style.NewTable("VERSION", "TIMESTAMP", "AUTHOR", "MESSAGE")

	for _, item := range items {
		version := strconv.FormatInt(item.GetGeneration(), 10)
		timestamp := annotationOrEmpty(item, "grafana.app/updatedTimestamp")
		author := annotationOrEmpty(item, "grafana.app/updatedBy")
		message := annotationOrEmpty(item, "grafana.app/message")
		t.Row(version, timestamp, author, message)
	}

	return t.Render(w)
}

// annotationOrEmpty returns the value of the named annotation, or "" if absent.
func annotationOrEmpty(item unstructured.Unstructured, key string) string {
	ann := item.GetAnnotations()
	if ann == nil {
		return ""
	}
	return ann[key]
}

// toVersionsSlice normalises the various input types into []unstructured.Unstructured.
func toVersionsSlice(v any) ([]unstructured.Unstructured, error) {
	switch val := v.(type) {
	case []unstructured.Unstructured:
		return val, nil
	case *unstructured.UnstructuredList:
		if val == nil {
			return nil, nil
		}
		return val.Items, nil
	case unstructured.UnstructuredList:
		return val.Items, nil
	default:
		return nil, fmt.Errorf("versionsTableCodec: unsupported type %T", v)
	}
}
