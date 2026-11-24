package adapter

import (
	"github.com/grafana/gcx/internal/resources"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PullFieldsStripper removes registered spec fields from saved pull manifests.
// Get and list output retain these fields.
type PullFieldsStripper struct{}

func (PullFieldsStripper) Process(res *resources.Resource) error {
	gvk := resources.NormalizeGVK(res.Object.GroupVersionKind())
	for _, reg := range registrations {
		if reg.GVK == gvk {
			for _, field := range reg.PullStripFields {
				unstructured.RemoveNestedField(res.Object.Object, "spec", field)
			}
			break
		}
	}
	return nil
}
