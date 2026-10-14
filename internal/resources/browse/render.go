package browse

import (
	"bytes"

	"github.com/grafana/gcx/internal/format"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// RenderObject renders an object as YAML for the preview pane. Server-side
// bookkeeping (metadata.managedFields) is dropped; the input is not modified.
func RenderObject(obj unstructured.Unstructured) (string, error) {
	c := obj.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	return renderYAML(c.Object)
}

func renderYAML(v any) (string, error) {
	var buf bytes.Buffer
	if err := format.NewYAMLCodec().Encode(&buf, v); err != nil {
		return "", err
	}
	return buf.String(), nil
}
