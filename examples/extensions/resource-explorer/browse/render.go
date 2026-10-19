package browse

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// RenderObject renders an object as YAML for the preview pane. Server-side
// bookkeeping (metadata.managedFields) is dropped; the input is not modified.
func RenderObject(obj unstructured.Unstructured) (string, error) {
	c := obj.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	return renderYAML(c.Object)
}

func renderYAML(v any) (string, error) {
	data, err := yaml.Marshal(v)
	return string(data), err
}
