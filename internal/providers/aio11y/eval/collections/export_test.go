package collections

import (
	"github.com/grafana/gcx/internal/resources/adapter"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// SpecToUnstructured renders a Collection through the typed CRUD's strip
// pipeline. Exposed for tests that assert server-managed fields are removed
// from spec output.
func SpecToUnstructured(item Collection, namespace string) (unstructured.Unstructured, error) {
	crud := &adapter.TypedCRUD[Collection]{
		Namespace:   namespace,
		StripFields: stripFields(),
		Descriptor:  StaticDescriptor(),
	}
	return crud.ToUnstructured(item)
}
